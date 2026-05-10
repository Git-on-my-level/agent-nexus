#!/usr/bin/env node
import { spawn, spawnSync } from "node:child_process";
import { mkdirSync } from "node:fs";
import http from "node:http";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import readline from "node:readline";

const scriptDir = dirname(fileURLToPath(import.meta.url));
const mcpRoot = resolve(scriptDir, "..");
const binDir = join(mcpRoot, ".tmp");
const binPath = join(binDir, "anx-mcp-smoke");

function fail(message) {
  console.error(`standalone-smoke: ${message}`);
  process.exit(1);
}

mkdirSync(binDir, { recursive: true });
const build = spawnSync("go", ["build", "-o", binPath, "./cmd/anx-mcp"], {
  cwd: mcpRoot,
  stdio: "inherit",
});
if (build.status !== 0) {
  fail("go build failed");
}

let mockServer;
let mockBaseURL;
if (process.env.ANX_MCP_SMOKE_MOCK === "1") {
  mockServer = http.createServer((req, res) => {
    if (req.headers.authorization !== "Bearer mock-token") {
      res.writeHead(401, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ code: "auth_required", message: "missing mock token" }));
      return;
    }
    if (req.method === "GET" && req.url.startsWith("/docs")) {
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ documents: [{ id: "doc_existing", title: "Existing smoke doc" }] }));
      return;
    }
    if (req.method === "POST" && req.url === "/docs") {
      let body = "";
      req.setEncoding("utf8");
      req.on("data", (chunk) => {
        body += chunk;
      });
      req.on("end", () => {
        const parsed = JSON.parse(body || "{}");
        res.writeHead(201, { "Content-Type": "application/json" });
        res.end(JSON.stringify({
          document: {
            id: "doc_created",
            title: parsed["document.title"],
            content_type: parsed.content_type,
          },
        }));
      });
      return;
    }
    res.writeHead(404, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ code: "not_found" }));
  });
  await new Promise((resolveListen) => mockServer.listen(0, "127.0.0.1", resolveListen));
  const address = mockServer.address();
  mockBaseURL = `http://127.0.0.1:${address.port}`;
  process.env.ANX_ACCESS_TOKEN = "mock-token";
}

const args = ["--log-level", process.env.ANX_MCP_SMOKE_LOG_LEVEL || "error"];
if (process.env.ANX_MCP_SMOKE_PROFILE) {
  args.push("--profile", process.env.ANX_MCP_SMOKE_PROFILE);
}
if (mockBaseURL || process.env.ANX_MCP_SMOKE_BASE_URL || process.env.ANX_BASE_URL) {
  args.push("--base-url", mockBaseURL || process.env.ANX_MCP_SMOKE_BASE_URL || process.env.ANX_BASE_URL);
}
if (process.env.ANX_MCP_SMOKE_AGENT || process.env.ANX_AGENT || mockBaseURL) {
  args.push("--agent", process.env.ANX_MCP_SMOKE_AGENT || process.env.ANX_AGENT || "mcp-smoke");
}

const child = spawn(binPath, args, {
  cwd: mcpRoot,
  env: process.env,
  stdio: ["pipe", "pipe", "pipe"],
});

child.stderr.on("data", (chunk) => {
  process.stderr.write(chunk);
});

const lines = readline.createInterface({ input: child.stdout });
const pending = new Map();
lines.on("line", (line) => {
  let message;
  try {
    message = JSON.parse(line);
  } catch {
    fail(`invalid JSON-RPC response: ${line}`);
  }
  const waiter = pending.get(message.id);
  if (!waiter) {
    fail(`unexpected response id ${message.id}: ${line}`);
  }
  pending.delete(message.id);
  waiter(message);
});

let nextID = 1;
function request(method, params) {
  const id = nextID++;
  const payload = { jsonrpc: "2.0", id, method };
  if (params !== undefined) {
    payload.params = params;
  }
  const timeout = Number(process.env.ANX_MCP_SMOKE_TIMEOUT_MS || 30000);
  return new Promise((resolveResponse, rejectResponse) => {
    const timer = setTimeout(() => {
      pending.delete(id);
      rejectResponse(new Error(`timed out waiting for ${method}`));
    }, timeout);
    pending.set(id, (message) => {
      clearTimeout(timer);
      if (message.error) {
        rejectResponse(new Error(`${method} failed: ${JSON.stringify(message.error)}`));
        return;
      }
      resolveResponse(message.result);
    });
    child.stdin.write(`${JSON.stringify(payload)}\n`);
  });
}

try {
  const init = await request("initialize");
  if (init?.serverInfo?.name !== "anx-mcp") {
    fail(`unexpected initialize result: ${JSON.stringify(init)}`);
  }

  const listed = await request("tools/list", { limit: 100 });
  const names = new Set((listed.tools || []).map((tool) => tool.name));
  for (const name of ["anx_docs_list", "anx_docs_create"]) {
    if (!names.has(name)) {
      fail(`tools/list did not include ${name}`);
    }
  }

  const read = await request("tools/call", {
    name: "anx_docs_list",
    arguments: { query: { limit: 5 } },
  });
  if (read?.structuredContent?.command_id !== "docs.list") {
    fail(`docs.list did not return structured content: ${JSON.stringify(read)}`);
  }

  const title = `MCP standalone smoke ${new Date().toISOString()}`;
  const write = await request("tools/call", {
    name: "anx_docs_create",
    arguments: {
      idempotency_key: `mcp-standalone-smoke-${Date.now()}`,
      body: {
        "document.title": title,
        "document.summary": "Created by the standalone anx-mcp smoke script.",
        content_type: "text",
        content: "Standalone anx-mcp smoke document.",
      },
    },
  });
  if (write?.structuredContent?.command_id !== "docs.create") {
    fail(`docs.create did not return structured content: ${JSON.stringify(write)}`);
  }

  console.log("standalone-smoke: ok initialize tools/list docs.list docs.create");
} catch (err) {
  fail(err.message);
} finally {
  child.stdin.end();
  child.kill("SIGTERM");
  if (mockServer) {
    mockServer.close();
  }
}
