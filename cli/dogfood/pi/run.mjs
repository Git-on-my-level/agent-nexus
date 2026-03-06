import fs from "node:fs";
import net from "node:net";
import path from "node:path";
import process from "node:process";
import { spawnSync, spawn } from "node:child_process";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const packageRoot = here;
const repoRoot = path.resolve(packageRoot, "../../..");

const scenarioConfigs = {
  "pilot-rescue": {
    roleLimit: 4,
    threadTitles: {
      main: "Pilot Rescue Sprint: NorthWave Launch Readiness",
      feedback: "Customer Escalation: NorthWave Pilot Feedback",
      delivery: "Delivery Plan: Pilot Fix + Rollout Sequencing",
    },
    documentId: "northwave-pilot-rescue-brief",
    artifactIds: {
      feedbackMatrix: "artifact-feedback-matrix",
      feedbackQuotes: "artifact-feedback-quotes",
      launchChecklist: "artifact-launch-checklist",
      riskRegister: "artifact-risk-register",
      pilotMetrics: "artifact-pilot-metrics",
    },
    commitmentTitles: {
      digestFix: "Patch pilot digest cards to include commitment owner and due date",
      dedupeFix: "Stop duplicate escalation thread creation on commitment updates",
      closurePack: "Publish pilot rescue brief and customer closure plan",
    },
    roles: [
      {
        name: "support-lead",
        focus: "Translate customer pain into concrete launch requirements and closure conditions.",
        primaryThreadKey: "feedback",
        relatedThreadKeys: ["main"],
        artifactIds: ["feedbackMatrix", "feedbackQuotes"],
        commitmentTitles: [],
        privateContext: [
          "NorthWave's sponsor will judge Friday readiness mostly on the digest owner/due-date fix.",
          "BriskPay can tolerate staged artifact timeline work if support noise drops immediately.",
          "Do not promise implementation details. Your job is to preserve customer truth and closure criteria.",
        ],
        deliverable: "Publish one actor_statement on the main thread that summarizes customer impact, must-have fixes for Friday, and what can wait one week.",
        eventSummary: "Support recommendation: customer-critical fixes for Friday pilot rescue",
        eventThreadKeys: ["main", "feedback"],
        eventIncludeDocument: false,
        requireDocsUpdate: false,
      },
      {
        name: "delivery-engineer",
        focus: "Define the minimum safe technical scope and call out what does not fit Friday.",
        primaryThreadKey: "delivery",
        relatedThreadKeys: ["main", "feedback"],
        artifactIds: ["riskRegister", "launchChecklist"],
        commitmentTitles: ["digestFix", "dedupeFix"],
        privateContext: [
          "The digest field omission is low-risk and should fit Friday.",
          "Duplicate escalation thread creation is moderate risk but can still fit as a narrow pilot-path fix.",
          "Artifact timeline visibility is not a safe Friday fix; recommend a documented follow-up instead of pretending it is solved.",
        ],
        deliverable: "Publish one actor_statement on the main thread with the minimum safe fix set, explicit out-of-scope items, and the technical risks.",
        eventSummary: "Delivery recommendation: minimum safe Friday scope for pilot rescue",
        eventThreadKeys: ["main", "delivery"],
        eventIncludeDocument: false,
        requireDocsUpdate: false,
      },
      {
        name: "project-manager",
        focus: "Sequence work, launch gates, and customer validation so Friday is either credible or explicitly slipped.",
        primaryThreadKey: "delivery",
        relatedThreadKeys: ["main", "feedback"],
        artifactIds: ["launchChecklist", "pilotMetrics"],
        commitmentTitles: ["digestFix", "dedupeFix", "closurePack"],
        privateContext: [
          "There is only one practical Friday launch window. If the rescue brief is not credible by 11:00 local time, the pilot should slip one week.",
          "Your job is sequencing and risk ownership, not product scope definition.",
          "A good answer names the exact gate, the owner for each dependency, and the slip condition.",
        ],
        deliverable: "Publish one actor_statement on the main thread with the launch gate, ownership, and the exact condition that would force a one-week slip.",
        eventSummary: "Project manager recommendation: Friday pilot gate and ownership plan",
        eventThreadKeys: ["main", "delivery"],
        eventIncludeDocument: false,
        requireDocsUpdate: false,
      },
      {
        name: "product-manager",
        focus: "Make the final launch recommendation and update the GTM rescue brief after reviewing the other roles' outputs.",
        primaryThreadKey: "main",
        relatedThreadKeys: ["feedback", "delivery"],
        artifactIds: ["pilotMetrics", "feedbackMatrix", "launchChecklist"],
        commitmentTitles: ["closurePack"],
        privateContext: [
          "You can approve a limited Friday pilot rescue, but you cannot promise a platform rewrite this week.",
          "Your recommendation should explicitly separate Friday scope from follow-up scope.",
          "Before posting the final event, re-read the main thread context and wait until support, delivery, and project management have each posted a recommendation.",
        ],
        deliverable: "Update the `northwave-pilot-rescue-brief` document, then publish the final actor_statement on the main thread referencing that document and making a clear go/no-go recommendation.",
        eventSummary: "Product decision: final NorthWave pilot rescue recommendation",
        eventThreadKeys: ["main", "feedback", "delivery"],
        eventIncludeDocument: true,
        requireDocsUpdate: true,
      },
    ],
  },
};

function parseArgs(argv) {
  const options = {
    scenario: "pilot-rescue",
    provider: "zai",
    model: "glm-5",
    baseUrl: "",
    reportDir: path.join(repoRoot, "cli", ".tmp", "pi-dogfood"),
    apiKey: "",
    apiKeyFile: "",
    oarBin: "",
    coreBin: "",
    maxSeconds: 900,
    agentCount: 4,
    agentPrefix: "pi-dogfood-agent",
  };

  for (let idx = 0; idx < argv.length; idx += 1) {
    const arg = argv[idx];
    if (arg === "--") {
      continue;
    }
    switch (arg) {
      case "--scenario":
        options.scenario = argv[++idx] ?? "";
        break;
      case "--provider":
        options.provider = argv[++idx] ?? "";
        break;
      case "--model":
        options.model = argv[++idx] ?? "";
        break;
      case "--base-url":
        options.baseUrl = argv[++idx] ?? "";
        break;
      case "--report-dir":
        options.reportDir = argv[++idx] ?? "";
        break;
      case "--api-key":
        options.apiKey = argv[++idx] ?? "";
        break;
      case "--api-key-file":
        options.apiKeyFile = argv[++idx] ?? "";
        break;
      case "--oar-bin":
        options.oarBin = argv[++idx] ?? "";
        break;
      case "--core-bin":
        options.coreBin = argv[++idx] ?? "";
        break;
      case "--max-seconds":
        options.maxSeconds = Number(argv[++idx] ?? "0");
        break;
      case "--agent-count":
        options.agentCount = Number(argv[++idx] ?? "0");
        break;
      case "--agent-prefix":
        options.agentPrefix = argv[++idx] ?? "";
        break;
      default:
        throw new Error(`unknown argument: ${arg}`);
    }
  }

  if (!options.apiKey && !options.apiKeyFile) {
    throw new Error("set --api-key or --api-key-file");
  }
  if (!scenarioConfigs[options.scenario]) {
    throw new Error(`unknown scenario: ${options.scenario}`);
  }
  if (!Number.isFinite(options.maxSeconds) || options.maxSeconds <= 0) {
    throw new Error("--max-seconds must be a positive number");
  }
  if (!Number.isFinite(options.agentCount) || options.agentCount < 1) {
    throw new Error("--agent-count must be at least 1");
  }
  if (!options.agentPrefix.trim()) {
    throw new Error("--agent-prefix is required");
  }
  return options;
}

function resolveApiKey(options) {
  if (options.apiKey.trim()) {
    return options.apiKey.trim();
  }
  return fs.readFileSync(path.resolve(packageRoot, options.apiKeyFile), "utf8").trim();
}

function runToken() {
  return new Date().toISOString().replace(/[-:]/g, "").replace(/\.\d+Z$/, "Z").replace("T", "T");
}

function ensureDir(dirPath) {
  fs.mkdirSync(dirPath, { recursive: true });
}

function writeFile(filePath, content) {
  ensureDir(path.dirname(filePath));
  fs.writeFileSync(filePath, content);
}

function renderScenario(content, baseUrl) {
  return content.replace(/`http:\/\/127\.0\.0\.1:8000`/g, `\`${baseUrl}\``);
}

function commandGuide(baseUrl, defaultUsername) {
  return `# OAR Command Guide

Use these exact command shapes. Prefer them over guessing.

Base URL:
- ${baseUrl}

Auth:
- Show auth subcommands: \`oar auth\`
- Register default profile: \`oar auth register --username ${defaultUsername}\`
- Verify current profile: \`oar auth whoami\`

Read workflow state:
- List threads: \`oar threads list\`
- Read thread: \`oar threads get --thread-id <thread-id>\`
- Read thread context: \`oar threads context --thread-id <thread-id>\`
- List inbox items: \`oar inbox list\`
- List artifacts: \`oar artifacts list --thread-id <thread-id>\`
- Read artifact metadata: \`oar artifacts get --artifact-id <artifact-id>\`
- Read artifact content: \`oar artifacts content --artifact-id <artifact-id>\`
- List commitments for a thread: \`oar commitments list --thread-id <thread-id> --status open\`
- Read a seeded brief document: \`oar docs get --document-id northwave-pilot-rescue-brief\`
- Update a document revision: \`oar docs update --document-id northwave-pilot-rescue-brief --from-file doc-update-template.json\`

Write workflow state:
- Edit \`event-template.json\` in place, then create the event: \`oar events create --from-file event-template.json\`

Working event type for this scenario:
- \`actor_statement\`
`;
}

function resultTemplate() {
  return `# Result

## Summary

## OAR Commands Attempted

## Friction

## Concrete Suggestions
`;
}

function valueFrom(object, ...keys) {
  for (const key of keys) {
    const value = object?.[key];
    if (value !== undefined && value !== null && value !== "") {
      return value;
    }
  }
  return "";
}

async function apiJSON(baseUrl, apiPath) {
  const response = await fetch(`${baseUrl}${apiPath}`);
  if (!response.ok) {
    throw new Error(`GET ${apiPath} failed with status ${response.status}`);
  }
  return response.json();
}

async function resolveSharedTargets(baseUrl, config) {
  const threadsResponse = await apiJSON(baseUrl, "/threads");
  const threads = Array.isArray(threadsResponse?.threads) ? threadsResponse.threads : [];
  const byTitle = Object.fromEntries(threads.map((thread) => [valueFrom(thread, "title", "summary"), thread]));

  const mainThread = byTitle[config.threadTitles.main];
  const feedbackThread = byTitle[config.threadTitles.feedback];
  const deliveryThread = byTitle[config.threadTitles.delivery];
  if (!mainThread?.id || !feedbackThread?.id || !deliveryThread?.id) {
    throw new Error("failed to resolve scenario threads");
  }

  const artifacts = {};
  for (const artifactId of Object.values(config.artifactIds)) {
    const response = await apiJSON(baseUrl, `/artifacts/${encodeURIComponent(artifactId)}`);
    artifacts[artifactId] = response?.artifact;
  }

  const commitmentsResponse = await apiJSON(baseUrl, "/commitments?status=open");
  const allCommitments = Array.isArray(commitmentsResponse?.commitments) ? commitmentsResponse.commitments : [];
  const commitmentsByTitle = Object.fromEntries(allCommitments.map((commitment) => [valueFrom(commitment, "title", "summary"), commitment]));

  const inboxResponse = await apiJSON(baseUrl, "/inbox");
  const inboxItems = Array.isArray(inboxResponse?.items) ? inboxResponse.items : [];

  const documentResponse = await apiJSON(baseUrl, `/docs/${encodeURIComponent(config.documentId)}`);

  return {
    threads: {
      main: mainThread,
      feedback: feedbackThread,
      delivery: deliveryThread,
    },
    artifacts,
    commitments: {
      digestFix: commitmentsByTitle[config.commitmentTitles.digestFix] ?? null,
      dedupeFix: commitmentsByTitle[config.commitmentTitles.dedupeFix] ?? null,
      closurePack: commitmentsByTitle[config.commitmentTitles.closurePack] ?? null,
      all: allCommitments,
    },
    inboxItems,
    document: {
      id: config.documentId,
      response: documentResponse,
    },
  };
}

function roleTargets(config, shared, role) {
  const primaryThread = shared.threads[role.primaryThreadKey];
  const relatedThreads = role.relatedThreadKeys.map((key) => shared.threads[key]).filter(Boolean);
  const roleArtifacts = role.artifactIds.map((key) => shared.artifacts[config.artifactIds[key]]).filter(Boolean);
  const roleCommitments = role.commitmentTitles.map((key) => shared.commitments[key]).filter(Boolean);
  const relevantThreadIds = new Set([primaryThread?.id, ...relatedThreads.map((thread) => thread.id)]);
  const relevantInboxItems = shared.inboxItems.filter((item) => relevantThreadIds.has(valueFrom(item, "thread_id", "threadId")));
  return {
    mainThread: shared.threads.main,
    primaryThread,
    relatedThreads,
    artifacts: roleArtifacts,
    commitments: roleCommitments,
    inboxItems: relevantInboxItems,
    document: shared.document,
  };
}

function eventTemplate(role, targets) {
  const threadKeyToThread = {
    main: targets.mainThread,
    feedback: [targets.primaryThread, ...targets.relatedThreads].find((thread) => thread.title.includes("Customer Escalation")),
    delivery: [targets.primaryThread, ...targets.relatedThreads].find((thread) => thread.title.includes("Delivery Plan")),
  };
  const refs = [];
  for (const threadKey of role.eventThreadKeys ?? []) {
    const thread = threadKeyToThread[threadKey];
    if (thread?.id) {
      refs.push(`thread:${thread.id}`);
    }
  }
  if (role.eventIncludeDocument) {
    refs.push(`document:${targets.document.id}`);
  }
  for (const artifact of targets.artifacts) {
    refs.push(`artifact:${artifact.id}`);
  }
  for (const commitment of targets.commitments) {
    refs.push(`commitment:${commitment.id}`);
  }
  const uniqueRefs = [...new Set(refs.filter(Boolean))];
  return `{
  "event": {
    "type": "actor_statement",
    "thread_id": "${targets.mainThread.id}",
    "refs": ${JSON.stringify(uniqueRefs, null, 6)},
    "summary": "${role.eventSummary}",
    "payload": {
      "recommendation": "Replace this with a concrete recommendation from your role.",
      "evidence": [
        "Replace with specific facts from the threads, artifacts, and commitments you inspected."
      ],
      "follow_ups": [
        "Replace with explicit next steps and owners."
      ]
    },
    "provenance": {
      "sources": [
        "inferred"
      ]
    }
  }
}
`;
}

function docUpdateTemplate(targets) {
  const headRevision = valueFrom(targets.document.response?.revision, "revision_id");
  return `{
  "if_base_revision": "${headRevision}",
  "refs": [
    "thread:${targets.mainThread.id}",
    "document:${targets.document.id}",
    "artifact:artifact-feedback-matrix",
    "artifact:artifact-launch-checklist"
  ],
  "content_type": "text",
  "content": "# NorthWave Pilot Rescue Brief\n\nStatus: replace with recommended status\n\nFriday scope:\n- replace with scoped fixes\n\nDeferred follow-up:\n- replace with follow-up work\n\nLaunch recommendation:\n- replace with go/no-go call and rationale\n\nCustomer closure plan:\n- replace with exact commitments to NorthWave and BriskPay\n"
}
`;
}

function targetsGuide(role, targets) {
  const lines = [
    "# Scenario Targets",
    "",
    "Use these resolved IDs directly. Do not spend turns rediscovering them.",
    "",
    `Shared goal thread: ${targets.mainThread.id}`,
    `Shared goal title: ${targets.mainThread.title}`,
    `Primary thread for your role: ${targets.primaryThread.id}`,
    `Primary thread title: ${targets.primaryThread.title}`,
    `Read shared goal thread: oar threads get --thread-id ${targets.mainThread.id}`,
    `Read shared goal context: oar threads context --thread-id ${targets.mainThread.id}`,
    `Read your primary thread: oar threads get --thread-id ${targets.primaryThread.id}`,
    `Read your primary thread context: oar threads context --thread-id ${targets.primaryThread.id}`,
  ];

  if (targets.relatedThreads.length > 0) {
    lines.push("", "Related threads:");
    for (const thread of targets.relatedThreads) {
      lines.push(`- ${thread.id} :: ${thread.title}`);
    }
  }

  if (targets.artifacts.length > 0) {
    lines.push("", "Artifacts to inspect:");
    for (const artifact of targets.artifacts) {
      lines.push(`- ${artifact.id} :: ${valueFrom(artifact, "summary", "title")}`);
      lines.push(`  metadata: oar artifacts get --artifact-id ${artifact.id}`);
      lines.push(`  content: oar artifacts content --artifact-id ${artifact.id}`);
    }
  }

  if (targets.commitments.length > 0) {
    lines.push("", "Commitments in scope:");
    for (const commitment of targets.commitments) {
      lines.push(`- ${commitment.id} :: ${valueFrom(commitment, "title", "summary")}`);
    }
  }

  if (targets.inboxItems.length > 0) {
    lines.push("", "Relevant inbox items:");
    for (const item of targets.inboxItems) {
      lines.push(`- ${valueFrom(item, "id")} :: ${valueFrom(item, "category", "kind", "type")} :: ${valueFrom(item, "title", "summary")}`);
    }
  }

  lines.push("", `Your deliverable: ${role.deliverable}`);
  if (role.requireDocsUpdate) {
    lines.push(
      `Document to update: ${targets.document.id}`,
      `Read it first: oar docs get --document-id ${targets.document.id}`,
      `Then update it: oar docs update --document-id ${targets.document.id} --from-file doc-update-template.json`,
    );
  }

  return `${lines.join("\n")}\n`;
}

function privateContextGuide(role) {
  const lines = [
    "# Role Context",
    "",
    `Role: ${role.name}`,
    `Focus: ${role.focus}`,
    "",
    "Private context and constraints:",
    ...role.privateContext.map((line) => `- ${line}`),
    "",
    `Deliverable: ${role.deliverable}`,
  ];
  return `${lines.join("\n")}\n`;
}

function buildGoBinary(runDir, providedPath, moduleDir, packageDir, outputName) {
  if (providedPath) {
    return path.resolve(packageRoot, providedPath);
  }
  const outPath = path.join(runDir, "bin", outputName);
  ensureDir(path.dirname(outPath));
  const result = spawnSync("go", ["build", "-o", outPath, packageDir], {
    cwd: path.join(repoRoot, moduleDir),
    stdio: "inherit",
  });
  if (result.status !== 0) {
    throw new Error(`failed to build ${outputName}`);
  }
  return outPath;
}

function buildOarBinary(runDir, providedPath) {
  return buildGoBinary(runDir, providedPath, "cli", "./cmd/oar", "oar");
}

function buildCoreBinary(runDir, providedPath) {
  return buildGoBinary(runDir, providedPath, "core", "./cmd/oar-core", "oar-core");
}

function piExecutable() {
  const binName = process.platform === "win32" ? "pi.cmd" : "pi";
  return path.join(packageRoot, "node_modules", ".bin", binName);
}

async function findFreePort() {
  return new Promise((resolve, reject) => {
    const server = net.createServer();
    server.on("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const address = server.address();
      if (!address || typeof address === "string") {
        server.close(() => reject(new Error("failed to allocate free port")));
        return;
      }
      const { port } = address;
      server.close((error) => {
        if (error) {
          reject(error);
          return;
        }
        resolve(port);
      });
    });
  });
}

async function waitForCore(baseUrl, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  let lastError = "unknown";
  while (Date.now() < deadline) {
    try {
      const response = await fetch(`${baseUrl}/health`);
      if (response.ok) {
        return;
      }
      lastError = `status ${response.status}`;
    } catch (error) {
      lastError = error instanceof Error ? error.message : String(error);
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw new Error(`core did not become healthy: ${lastError}`);
}

async function seedCore(baseUrl) {
  const seedScript = path.join(packageRoot, "seed", "seed-core.mjs");
  const result = spawnSync("node", [seedScript], {
    cwd: repoRoot,
    stdio: "inherit",
    env: {
      ...process.env,
      OAR_CORE_BASE_URL: baseUrl,
      OAR_FORCE_SEED: "1",
    },
  });
  if (result.status !== 0) {
    throw new Error("failed to seed core from CLI-owned mock data");
  }
}

async function startManagedCore(runDir, coreBin, requestedBaseUrl) {
  if (requestedBaseUrl) {
    await waitForCore(requestedBaseUrl, 20000);
    return {
      baseUrl: requestedBaseUrl,
      stop: async () => {},
      managed: false,
      workspaceDir: "",
      logPath: "",
    };
  }

  const workspaceDir = path.join(runDir, "core-workspace");
  const logPath = path.join(runDir, "core.log");
  const schemaPath = path.join(repoRoot, "contracts", "oar-schema.yaml");
  const host = "127.0.0.1";
  const port = await findFreePort();
  const baseUrl = `http://${host}:${port}`;
  ensureDir(workspaceDir);
  const logStream = fs.createWriteStream(logPath, { flags: "a" });

  const child = spawn(coreBin, [
    "--host",
    host,
    "--port",
    String(port),
    "--schema-path",
    schemaPath,
    "--workspace-root",
    workspaceDir,
  ], {
    cwd: path.join(repoRoot, "core"),
    env: process.env,
    stdio: ["ignore", "pipe", "pipe"],
  });

  child.stdout.on("data", (chunk) => {
    process.stdout.write(chunk);
    logStream.write(chunk);
  });
  child.stderr.on("data", (chunk) => {
    process.stderr.write(chunk);
    logStream.write(chunk);
  });

  const stop = async () => {
    if (child.exitCode !== null) {
      logStream.end();
      return;
    }
    child.kill("SIGTERM");
    await new Promise((resolve) => {
      const timeout = setTimeout(() => {
        if (child.exitCode === null) {
          child.kill("SIGKILL");
        }
      }, 5000);
      child.on("exit", () => {
        clearTimeout(timeout);
        logStream.end();
        resolve();
      });
    });
  };

  await Promise.race([
    waitForCore(baseUrl, 20000),
    new Promise((_, reject) => {
      child.once("error", reject);
      child.once("exit", (code, signal) => {
        reject(new Error(`managed core exited before ready (code=${code ?? "null"} signal=${signal ?? "none"})`));
      });
    }),
  ]);
  await seedCore(baseUrl);

  return {
    baseUrl,
    stop,
    managed: true,
    workspaceDir,
    logPath,
  };
}

function agentWorkspaceDir(runDir, agentCount, agentId) {
  if (agentCount === 1) {
    return path.join(runDir, "workspace");
  }
  return path.join(runDir, "workspace", agentId);
}

function agentEventsPath(runDir, agentCount, agentId) {
  if (agentCount === 1) {
    return path.join(runDir, "events.jsonl");
  }
  return path.join(runDir, `events-${agentId}.jsonl`);
}

function agentResultPath(workspaceDir) {
  return path.join(workspaceDir, "result.md");
}

async function runPiAgent({
  runDir,
  piHomeDir,
  oarBin,
  coreBaseUrl,
  provider,
  model,
  apiKey,
  maxSeconds,
  agentId,
  agentCount,
  agentUsername,
  scenarioMarkdown,
  role,
  targets,
}) {
  const workspaceDir = agentWorkspaceDir(runDir, agentCount, agentId);
  const eventsPath = agentEventsPath(runDir, agentCount, agentId);
  const resultPath = agentResultPath(workspaceDir);
  ensureDir(workspaceDir);

  const agentsContent = `# Pi Dogfood Run

You are evaluating the OAR CLI, not editing the repository.

Rules:
- Use the \`oar\` binary on PATH for all OAR interactions.
- Do not use \`curl\` for OAR API calls.
- Do not edit repository source files.
- Keep notes and artifacts inside the current working directory.
- Register with username \`${agentUsername}\`.
- Before finishing, write \`result.md\` containing:
  - summary
  - oar commands attempted
  - friction
  - concrete suggestions

Agent role:
- Agent id: ${agentId}
- Role: ${role.name}
- Focus: ${role.focus}

Environment:
- OAR base URL: ${coreBaseUrl}
- Working directory: ${workspaceDir}
- Scenario brief: ./SCENARIO.md
- Command guide: ./COMMANDS.md
- Scenario targets: ./TARGETS.md
- Role context: ./ROLE_CONTEXT.md
- Event template: ./event-template.json
- Document update template (if present): ./doc-update-template.json
- Result template: ./result-template.md
`;
  writeFile(path.join(workspaceDir, "AGENTS.md"), agentsContent);
  writeFile(path.join(workspaceDir, "SCENARIO.md"), scenarioMarkdown);
  writeFile(path.join(workspaceDir, "COMMANDS.md"), commandGuide(coreBaseUrl, agentUsername));
  writeFile(path.join(workspaceDir, "TARGETS.md"), targetsGuide(role, targets));
  writeFile(path.join(workspaceDir, "ROLE_CONTEXT.md"), privateContextGuide(role));
  writeFile(path.join(workspaceDir, "event-template.json"), eventTemplate(role, targets));
  if (role.requireDocsUpdate) {
    writeFile(path.join(workspaceDir, "doc-update-template.json"), docUpdateTemplate(targets));
  }
  writeFile(path.join(workspaceDir, "result-template.md"), resultTemplate());

  const prompt = role.requireDocsUpdate
    ? "Read SCENARIO.md, COMMANDS.md, TARGETS.md, and ROLE_CONTEXT.md. Execute your role with the real oar CLI. Update doc-update-template.json in place and use it to update the seeded rescue brief before posting your final event. Edit event-template.json in place, create the event from that file, write result.md, and then give a short final summary."
    : "Read SCENARIO.md, COMMANDS.md, TARGETS.md, and ROLE_CONTEXT.md. Execute your role with the real oar CLI. Edit event-template.json in place, create the event from that file, write result.md, and then give a short final summary.";

  const piArgs = [
    "--print",
    "--mode",
    "json",
    "--provider",
    provider,
    "--model",
    model,
    "--api-key",
    apiKey,
    "--no-session",
    "--tools",
    "read,bash,edit,write,grep,find,ls",
    "--no-extensions",
    "--no-skills",
    "--no-prompt-templates",
    "--no-themes",
    "--append-system-prompt",
    `Use oar on PATH for OAR interactions. Do not use curl. Work only inside the current directory. Register with username ${agentUsername}.`,
    "@SCENARIO.md",
    prompt,
  ];

  const eventStream = fs.createWriteStream(eventsPath, { flags: "a" });
  const homeDir = path.join(runDir, `home-${agentId}`);
  ensureDir(homeDir);
  const env = {
    ...process.env,
    HOME: homeDir,
    PATH: `${path.dirname(oarBin)}${path.delimiter}${process.env.PATH ?? ""}`,
    PI_CODING_AGENT_DIR: path.join(piHomeDir, agentId),
    OAR_BASE_URL: coreBaseUrl,
  };
  ensureDir(env.PI_CODING_AGENT_DIR);

  return new Promise((resolve, reject) => {
    const child = spawn(piExecutable(), piArgs, {
      cwd: workspaceDir,
      env,
      stdio: ["ignore", "pipe", "pipe"],
    });

    const timeout = setTimeout(() => {
      if (child.exitCode === null) {
        child.kill("SIGTERM");
      }
    }, maxSeconds * 1000);

    child.stdout.on("data", (chunk) => {
      process.stdout.write(chunk);
      eventStream.write(chunk);
    });
    child.stderr.on("data", (chunk) => {
      process.stderr.write(chunk);
    });
    child.on("error", (error) => {
      clearTimeout(timeout);
      eventStream.end();
      reject(error);
    });
    child.on("exit", (code, signal) => {
      clearTimeout(timeout);
      eventStream.end();
      if (signal) {
        reject(new Error(`${agentId}: pi terminated by signal ${signal}`));
        return;
      }
      if (code !== 0) {
        reject(new Error(`${agentId}: pi exited with code ${code}`));
        return;
      }
      resolve({
        agentId,
        agentUsername,
        workspaceDir,
        eventsPath,
        resultPath,
      });
    });
  });
}

async function main() {
  const options = parseArgs(process.argv.slice(2));
  const config = scenarioConfigs[options.scenario];
  if (options.agentCount > config.roleLimit) {
    throw new Error(`scenario ${options.scenario} supports at most ${config.roleLimit} agents`);
  }

  const apiKey = resolveApiKey(options);
  const scenarioPath = path.join(packageRoot, "scenarios", `${options.scenario}.md`);
  if (!fs.existsSync(scenarioPath)) {
    throw new Error(`scenario file not found: ${scenarioPath}`);
  }

  const runId = `${options.scenario}-${runToken()}`;
  const runDir = path.join(path.resolve(options.reportDir), runId);
  const piHomeDir = path.join(runDir, "pi-home");
  ensureDir(piHomeDir);

  const oarBin = buildOarBinary(runDir, options.oarBin);
  const coreBin = buildCoreBinary(runDir, options.coreBin);
  const core = await startManagedCore(runDir, coreBin, options.baseUrl);
  const scenarioContent = fs.readFileSync(scenarioPath, "utf8");
  const renderedScenario = renderScenario(scenarioContent, core.baseUrl);
  const sharedTargets = await resolveSharedTargets(core.baseUrl, config);
  const roles = config.roles.slice(0, options.agentCount);

  console.log(`pi dogfood run: ${runId}`);
  console.log(`base url: ${core.baseUrl}`);
  console.log(`agents: ${options.agentCount}`);

  let agentRuns = [];
  try {
    const pendingAgents = roles.map((role, agentIndex) => {
      const agentId = `agent-${String(agentIndex + 1).padStart(2, "0")}`;
      const agentUsername = `${options.agentPrefix}-${role.name}`;
      return runPiAgent({
        runDir,
        piHomeDir,
        oarBin,
        coreBaseUrl: core.baseUrl,
        provider: options.provider,
        model: options.model,
        apiKey,
        maxSeconds: options.maxSeconds,
        agentId,
        agentCount: options.agentCount,
        agentUsername,
        scenarioMarkdown: renderedScenario,
        role,
        targets: roleTargets(config, sharedTargets, role),
      });
    });

    const settled = await Promise.allSettled(pendingAgents);
    agentRuns = settled.map((result, index) => {
      if (result.status === "fulfilled") {
        return { status: "ok", role: roles[index].name, ...result.value };
      }
      return {
        status: "failed",
        role: roles[index].name,
        agentId: `agent-${String(index + 1).padStart(2, "0")}`,
        error: result.reason instanceof Error ? result.reason.message : String(result.reason),
      };
    });

    const failedAgents = agentRuns.filter((agent) => agent.status !== "ok");
    if (failedAgents.length > 0) {
      throw new Error(`pi dogfood failed for ${failedAgents.map((agent) => `${agent.agentId}: ${agent.error}`).join(", ")}`);
    }
  } finally {
    await core.stop();
  }

  const metadata = {
    run_id: runId,
    scenario: options.scenario,
    provider: options.provider,
    model: options.model,
    base_url: core.baseUrl,
    managed_core: core.managed,
    core_workspace_dir: core.workspaceDir,
    core_log_path: core.logPath,
    agent_count: options.agentCount,
    targets: sharedTargets,
    agents: agentRuns,
    oar_bin: oarBin,
    core_bin: coreBin,
  };
  writeFile(path.join(runDir, "run-metadata.json"), `${JSON.stringify(metadata, null, 2)}\n`);

  for (const agent of agentRuns) {
    if (agent.status !== "ok") {
      continue;
    }
    console.log(`workspace (${agent.agentId}): ${agent.workspaceDir}`);
    console.log(`events (${agent.agentId}): ${agent.eventsPath}`);
    if (fs.existsSync(agent.resultPath)) {
      console.log(`result (${agent.agentId}): ${agent.resultPath}`);
    } else {
      console.log(`warning: ${agent.agentId} did not write result.md`);
    }
  }
  console.log(`metadata: ${path.join(runDir, "run-metadata.json")}`);
}

main().catch((error) => {
  console.error(error.message);
  process.exit(1);
});
