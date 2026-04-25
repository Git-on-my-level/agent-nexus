#!/usr/bin/env node

import { spawn } from "node:child_process";
import http from "node:http";
import { mkdir, readFile, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { setTimeout as delay } from "node:timers/promises";

import { chromium } from "@playwright/test";
import pixelmatch from "pixelmatch";
import { PNG } from "pngjs";

import {
  QA_ACTORS,
  QA_ARTIFACTS,
  QA_ASK_ITEM,
  QA_AUTH_AUDIT,
  QA_AUTH_AGENT,
  QA_DOCUMENTS,
  QA_EVENTS,
  QA_FIXED_NOW_ISO,
  QA_HOME_HANDOFF_PARTIAL_MARK_ISO,
  QA_HOME_HANDOFF_ZERO_MARK_ISO,
  QA_HOSTED_ACCOUNT,
  QA_HOSTED_BILLING_SUMMARY,
  QA_INVITES,
  QA_HOSTED_ORGS,
  QA_HOSTED_WORKSPACES,
  QA_INBOX_POPULATED,
  QA_PRINCIPALS,
  QA_SECRETS,
  QA_TOPICS,
  QA_BOARDS,
  filterByQuery,
} from "../tests/fixtures/qa-seed.js";
import { getExpectedCommandRegistryDigest } from "../src/lib/commandRegistryDigest.js";
import { EXPECTED_SCHEMA_VERSION } from "../src/lib/config.js";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const projectRoot = path.resolve(__dirname, "..");

const DEFAULT_VIEWPORT = { width: 1440, height: 900 };
const DEFAULT_PORT = Number(process.env.QA_VISUAL_PORT ?? 4273);
const DEFAULT_CORE_PORT = Number(process.env.QA_VISUAL_CORE_PORT ?? 8000);
const DEFAULT_THRESHOLD_RATIO = 0.001;
/** Git-tracked PNGs for `qa:diff` in CI (`.qa-baseline/` is gitignored for local captures). */
const QA_BASELINE_DIR = path.join(
  projectRoot,
  "tests",
  "fixtures",
  "qa-visual-baseline",
);
const QA_CURRENT_DIR = path.join(projectRoot, ".qa-current");
const QA_DIFF_DIR = path.join(projectRoot, ".qa-diff");
const QA_HOME_HANDOFF_STORAGE_KEY = "anx.home.handoff.lastRead.v1.local.local";

const QA_SCENES = [
  {
    name: "hosted-start",
    path: "/hosted/start",
    hostedMode: "pending-start",
    waitFor: async (page) => {
      await page.waitForSelector("text=Redirecting…");
    },
  },
  {
    name: "hosted-signin",
    path: "/hosted/signin",
    hostedMode: "public",
    waitFor: async (page) => {
      await page.waitForSelector("text=Welcome back");
    },
  },
  {
    name: "hosted-signup",
    path: "/hosted/signup",
    hostedMode: "public",
    waitFor: async (page) => {
      await page.waitForSelector("text=Create your account");
    },
  },
  {
    name: "hosted-dashboard",
    path: "/hosted/dashboard",
    hostedMode: "authed-dashboard",
    waitFor: async (page) => {
      await page.waitForSelector("text=Northwind Autonomy workspaces");
    },
  },
  {
    name: "hosted-organizations",
    path: "/hosted/organizations",
    hostedMode: "authed-dashboard",
    waitFor: async (page) => {
      await page.waitForSelector('h1:has-text("Organizations")');
    },
  },
  {
    name: "hosted-organizations-billing",
    path: "/hosted/organizations/org_qa_primary/billing",
    hostedMode: "authed-dashboard",
    waitFor: async (page) => {
      await page.waitForSelector("text=Manage in Stripe");
    },
  },
  {
    name: "hosted-organization-detail",
    path: "/hosted/organizations/org_qa_primary",
    hostedMode: "authed-dashboard",
    waitFor: async (page) => {
      await page.waitForSelector("text=Manage billing");
    },
  },
  {
    name: "hosted-organization-usage",
    path: "/hosted/organizations/org_qa_primary/usage",
    hostedMode: "authed-dashboard",
    waitFor: async (page) => {
      await page.waitForSelector('h1:has-text("Usage")');
    },
  },
  {
    name: "hosted-billing-return",
    path: "/hosted/billing/return?session_id=cs_mock_qa_1",
    hostedMode: "authed-dashboard",
    waitFor: async (page) => {
      await page.waitForURL(
        /\/hosted\/organizations\/org_qa_primary\/billing\?activating=1$/,
      );
      await page.waitForSelector("text=Manage in Stripe");
    },
  },
  {
    name: "hosted-workspaces-new",
    path: "/hosted/workspaces/new",
    hostedMode: "authed-dashboard",
    waitFor: async (page) => {
      await page.waitForSelector("text=Create a workspace");
    },
  },
  {
    name: "hosted-onboarding-organization",
    path: "/hosted/onboarding/organization",
    hostedMode: "onboarding-organization",
    waitFor: async (page) => {
      await page.waitForSelector("text=Name your organization");
    },
  },
  {
    name: "hosted-onboarding-workspace",
    path: "/hosted/onboarding/workspace",
    hostedMode: "onboarding-workspace",
    waitFor: async (page) => {
      await page.waitForSelector("text=Name your first workspace");
    },
  },
  {
    name: "workspace-home-handoff-first-run",
    path: "/o/local/w/local",
    workspaceMode: "workspace-default",
    waitFor: async (page) => {
      await page.waitForSelector('[data-testid="home-change-counts"]');
      await page.waitForSelector(
        "text=Showing all recent workspace changes until you mark this handoff read.",
      );
    },
  },
  {
    name: "workspace-home-handoff-populated",
    path: "/o/local/w/local",
    workspaceMode: "workspace-default",
    localStorage: {
      [QA_HOME_HANDOFF_STORAGE_KEY]: QA_HOME_HANDOFF_PARTIAL_MARK_ISO,
    },
    waitFor: async (page) => {
      await page.waitForSelector('[data-testid="home-change-counts"]');
      await page.waitForSelector("text=Since you marked this workspace read");
    },
  },
  {
    name: "workspace-home-handoff-empty",
    path: "/o/local/w/local",
    workspaceMode: "workspace-default",
    localStorage: {
      [QA_HOME_HANDOFF_STORAGE_KEY]: QA_HOME_HANDOFF_ZERO_MARK_ISO,
    },
    waitFor: async (page) => {
      await page.waitForSelector('[data-testid="home-change-counts"]');
      await page.waitForSelector(
        "text=Nothing new since you marked this workspace read.",
      );
    },
  },
  {
    name: "workspace-inbox-empty",
    path: "/o/local/w/local/inbox",
    workspaceMode: "inbox-empty",
    waitFor: async (page) => {
      await page.waitForSelector("text=Inbox is clear");
    },
  },
  {
    name: "workspace-inbox-populated",
    path: "/o/local/w/local/inbox",
    workspaceMode: "inbox-populated",
    /** CI Linux/Chromium text shaping can drift slightly vs capture host. */
    thresholdRatio: 0.002,
    waitFor: async (page) => {
      await page.waitForSelector('[data-testid="inbox-card-inbox-ask-auth"]');
    },
  },
  {
    name: "workspace-inbox-loading",
    path: "/o/local/w/local/inbox",
    workspaceMode: "inbox-loading",
    waitFor: async (page) => {
      await page.waitForSelector(".animate-pulse");
    },
  },
  {
    name: "workspace-inbox-error",
    path: "/o/local/w/local/inbox",
    workspaceMode: "inbox-error",
    waitFor: async (page) => {
      await page.waitForSelector('[role="alert"]');
    },
  },
  {
    name: "workspace-inbox-fresh",
    path: "/o/local/w/local/inbox",
    workspaceMode: "inbox-fresh",
    waitFor: async (page) => {
      await page.waitForSelector("text=Inbox is clear");
    },
  },
  {
    name: "workspace-capture-ui",
    path: "/o/local/w/local/inbox/inbox-ask-auth",
    workspaceMode: "capture-ui",
    thresholdRatio: 0.018,
    waitFor: async (page) => {
      await page.waitForSelector("text=Context the agent saw");
    },
  },
  {
    name: "workspace-capture-degraded",
    path: "/o/local/w/local/inbox/inbox-ask-auth",
    workspaceMode: "capture-degraded",
    waitFor: async (page) => {
      await page.waitForSelector("text=Context didn't load.");
    },
  },
  {
    name: "workspace-topics",
    path: "/o/local/w/local/topics",
    workspaceMode: "workspace-default",
    waitFor: async (page) => {
      await page.waitForSelector('h1:has-text("Topics")');
    },
  },
  {
    name: "workspace-boards",
    path: "/o/local/w/local/boards",
    workspaceMode: "workspace-default",
    waitFor: async (page) => {
      await page.waitForSelector('h1:has-text("Boards")');
      await page.waitForSelector("text=Launch control");
    },
  },
  {
    name: "workspace-artifacts",
    path: "/o/local/w/local/artifacts",
    workspaceMode: "workspace-default",
    waitFor: async (page) => {
      await page.waitForSelector('h1:has-text("Artifacts")');
    },
  },
  {
    name: "workspace-docs",
    path: "/o/local/w/local/docs",
    workspaceMode: "workspace-default",
    waitFor: async (page) => {
      await page.waitForSelector('h1:has-text("Docs")');
    },
  },
  {
    name: "workspace-settings",
    path: "/o/local/w/local/more",
    workspaceMode: "workspace-default",
    waitFor: async (page) => {
      await page.waitForSelector("text=Settings");
    },
  },
  {
    name: "workspace-access",
    path: "/o/local/w/local/access",
    workspaceMode: "workspace-default",
    thresholdRatio: 0.02,
    waitFor: async (page) => {
      await page.waitForSelector("text=Create invite");
    },
  },
  {
    name: "workspace-secrets",
    path: "/o/local/w/local/secrets",
    workspaceMode: "workspace-default",
    waitFor: async (page) => {
      await page.waitForSelector("text=OPENAI_API_KEY");
    },
  },
  {
    name: "command-palette-open",
    path: "/o/local/w/local/inbox",
    workspaceMode: "inbox-populated",
    waitFor: async (page) => {
      await page.waitForSelector('[data-testid="inbox-card-inbox-ask-auth"]');
      await page.click(".shell-search-trigger");
      await page.fill(".cmd-input", "launch");
      await page.waitForSelector(".cmd-result-row");
    },
  },
  {
    name: "confirm-modal-open",
    path: "/o/local/w/local/topics",
    workspaceMode: "workspace-default",
    waitFor: async (page) => {
      await page.waitForSelector('h1:has-text("Topics")');
      await page.locator('button[aria-label="Archive"]').first().click();
      await page.waitForSelector("text=Archive topic");
    },
  },
];

function parseCliArgs(argv) {
  const args = argv.slice(2);
  const options = {
    mode: "diff",
    port: DEFAULT_PORT,
    thresholdRatio: DEFAULT_THRESHOLD_RATIO,
    outDir: QA_CURRENT_DIR,
    json: false,
  };

  for (let index = 0; index < args.length; index += 1) {
    const arg = args[index];
    switch (arg) {
      case "--baseline":
        options.mode = "baseline";
        options.outDir = QA_BASELINE_DIR;
        break;
      case "--diff":
        options.mode = "diff";
        options.outDir = QA_CURRENT_DIR;
        break;
      case "--port":
        options.port = Number(args[index + 1] ?? DEFAULT_PORT);
        index += 1;
        break;
      case "--out":
        options.outDir = path.resolve(args[index + 1]);
        index += 1;
        break;
      case "--threshold":
        options.thresholdRatio = Number(
          args[index + 1] ?? DEFAULT_THRESHOLD_RATIO,
        );
        index += 1;
        break;
      case "--json":
        options.json = true;
        break;
      case "--help":
      case "-h":
        printUsage();
        process.exit(0);
        break;
      default:
        throw new Error(`Unknown option: ${arg}`);
    }
  }

  return options;
}

function printUsage() {
  console.log(
    `
QA Visual Harness

Usage:
  node scripts/qa-visual.mjs --baseline
  node scripts/qa-visual.mjs --diff

Options:
  --baseline            Capture the canonical baseline into tests/fixtures/qa-visual-baseline/
  --diff                Capture current screenshots and diff against that baseline
  --out <dir>           Override the output directory for fresh captures
  --port <port>         Preview server port (default: ${DEFAULT_PORT})
  --threshold <ratio>   Max differing-pixel ratio before failing (default: ${DEFAULT_THRESHOLD_RATIO}; some scenes override)
  --json                Emit machine-readable JSON summary
`.trim(),
  );
}

function normalizePathname(pathname) {
  if (!pathname || pathname === "/") {
    return "/";
  }
  return pathname.endsWith("/") ? pathname.slice(0, -1) : pathname;
}

function getLimit(searchParams, fallback = 200) {
  const raw = Number.parseInt(String(searchParams.get("limit") ?? ""), 10);
  if (!Number.isFinite(raw) || raw <= 0) {
    return fallback;
  }
  return raw;
}

function sliceByLimit(items, searchParams) {
  return items.slice(0, getLimit(searchParams, items.length));
}

function qaBoardListRows(boards) {
  return boards.map((b) => {
    const { board_summary, projection_freshness, ...boardFields } = b;
    const cols = board_summary?.cards_by_column ?? {};
    const cardCount = Object.values(cols).reduce(
      (acc, n) => acc + Number(n ?? 0),
      0,
    );
    const docCount = Array.isArray(boardFields.document_refs)
      ? boardFields.document_refs.length
      : 0;
    return {
      board: { ...boardFields, projection_freshness },
      summary: {
        card_count: cardCount,
        cards_by_column: cols,
        unresolved_card_count: cardCount,
        resolved_card_count: 0,
        document_count: docCount,
        latest_activity_at: board_summary?.latest_activity_at ?? null,
        has_document_refs: docCount > 0,
      },
    };
  });
}

function createWorkspaceScenario(mode) {
  switch (mode) {
    case "inbox-empty":
      return { inboxState: "empty", askState: "ok" };
    case "inbox-populated":
      return { inboxState: "populated", askState: "ok" };
    case "inbox-loading":
      return { inboxState: "loading", askState: "ok" };
    case "inbox-error":
      return { inboxState: "error", askState: "ok" };
    case "inbox-fresh":
      return { inboxState: "fresh", askState: "ok" };
    case "capture-ui":
      return { inboxState: "populated", askState: "ok" };
    case "capture-degraded":
      return { inboxState: "populated", askState: "error" };
    default:
      return { inboxState: "populated", askState: "ok" };
  }
}

function createHostedScenario(mode) {
  switch (mode) {
    case "pending-start":
      return { accountState: "pending", organizations: [], workspaces: [] };
    case "public":
      return { accountState: "unauthed", organizations: [], workspaces: [] };
    case "onboarding-organization":
      return { accountState: "authed", organizations: [], workspaces: [] };
    case "onboarding-workspace":
      return {
        accountState: "authed",
        organizations: [QA_HOSTED_ORGS[0]],
        workspaces: [],
      };
    case "authed-dashboard":
    default:
      return {
        accountState: "authed",
        organizations: QA_HOSTED_ORGS,
        workspaces: QA_HOSTED_WORKSPACES,
      };
  }
}

function jsonResponse(status, body) {
  return {
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  };
}

async function waitForServer(baseUrl, timeoutMs = 30_000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(baseUrl, {
        signal: AbortSignal.timeout(2_000),
      });
      if (response.ok || response.status < 500) {
        return;
      }
    } catch {
      // retry until timeout
    }
    await delay(250);
  }
  throw new Error(`Timed out waiting for preview server at ${baseUrl}`);
}

async function runCommand(cmd, args, options = {}) {
  const child = spawn(cmd, args, {
    cwd: projectRoot,
    stdio: "inherit",
    shell: false,
    ...options,
  });

  await new Promise((resolve, reject) => {
    child.on("error", reject);
    child.on("exit", (code) => {
      if (code === 0) {
        resolve();
        return;
      }
      reject(new Error(`${cmd} ${args.join(" ")} exited with code ${code}`));
    });
  });
}

async function ensureBuild() {
  await runCommand("pnpm", ["exec", "vite", "build"]);
}

async function startMockCoreServer(port = DEFAULT_CORE_PORT) {
  const commandRegistryDigest = await getExpectedCommandRegistryDigest();

  const server = http.createServer((request, response) => {
    const url = new URL(request.url ?? "/", `http://127.0.0.1:${port}`);
    if (request.method === "GET" && url.pathname === "/meta/handshake") {
      response.writeHead(200, { "content-type": "application/json" });
      response.end(
        JSON.stringify({
          core_version: "qa-mock-core",
          api_version: "qa",
          schema_version: EXPECTED_SCHEMA_VERSION,
          command_registry_digest: commandRegistryDigest,
          min_cli_version: "",
          recommended_cli_version: "",
          cli_download_url: "",
          core_instance_id: "qa-baseline",
          dev_actor_mode: false,
          human_auth_mode: "workspace_local",
        }),
      );
      return;
    }

    if (request.method === "GET" && url.pathname === "/version") {
      response.writeHead(200, { "content-type": "application/json" });
      response.end(
        JSON.stringify({
          schema_version: EXPECTED_SCHEMA_VERSION,
          command_registry_digest: commandRegistryDigest,
        }),
      );
      return;
    }

    response.writeHead(404, { "content-type": "application/json" });
    response.end(JSON.stringify({ error: { message: "not found" } }));
  });

  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(port, "127.0.0.1", resolve);
  });

  return {
    async stop() {
      await new Promise((resolve, reject) => {
        server.close((error) => {
          if (error) {
            reject(error);
            return;
          }
          resolve();
        });
      });
    },
  };
}

async function startBuiltUiServer(port) {
  const qaWorkspaceCatalog = JSON.stringify([
    {
      organizationSlug: "local",
      slug: "local",
      label: "Local QA Workspace",
      coreBaseUrl: `http://127.0.0.1:${DEFAULT_CORE_PORT}`,
    },
  ]);
  const child = spawn("node", ["build/index.js"], {
    cwd: projectRoot,
    stdio: "inherit",
    shell: false,
    env: {
      ...process.env,
      HOST: "127.0.0.1",
      PORT: String(port),
      ORIGIN: `http://127.0.0.1:${port}`,
      ANX_WORKSPACES: qaWorkspaceCatalog,
      ANX_UI_CSP_SCRIPT_SRC_EXTRA: "'unsafe-inline'",
    },
  });

  const baseUrl = `http://127.0.0.1:${port}`;
  try {
    await waitForServer(baseUrl);
  } catch (error) {
    child.kill("SIGTERM");
    throw error;
  }

  return {
    baseUrl,
    async stop() {
      child.kill("SIGTERM");
      await Promise.race([
        new Promise((resolve) => child.once("exit", resolve)),
        delay(5_000).then(() => {
          child.kill("SIGKILL");
        }),
      ]);
    },
  };
}

function buildSceneUrl(baseUrl, scenePath) {
  const separator = scenePath.includes("?") ? "&" : "?";
  return `${baseUrl}${scenePath}${separator}qa=1`;
}

async function installQaEnvironment(page, scene) {
  const workspaceScenario = createWorkspaceScenario(scene.workspaceMode);

  await page.addInitScript(
    ({ fixedNowIso, workspaceScenario, sceneLocalStorage }) => {
      const fixedNowMs = Date.parse(fixedNowIso);
      const RealDate = Date;

      class QADate extends RealDate {
        constructor(...args) {
          if (args.length === 0) {
            super(fixedNowMs);
            return;
          }
          super(...args);
        }

        static now() {
          return fixedNowMs;
        }
      }

      Object.setPrototypeOf(QADate, RealDate);
      globalThis.Date = QADate;

      let seed = 1337;
      Math.random = () => {
        seed = (seed * 48271) % 0x7fffffff;
        return seed / 0x7fffffff;
      };

      localStorage.setItem("oar_hosted_active_org_id", "org_qa_primary");
      localStorage.removeItem("anx.tour.inbox.v1.local");
      localStorage.removeItem("anx.tour.inbox.v1.local.firstSeen");
      if (workspaceScenario.inboxState !== "fresh") {
        localStorage.setItem("anx.tour.inbox.v1.local", "dismissed");
      }

      for (const [key, value] of Object.entries(sceneLocalStorage ?? {})) {
        if (value == null) {
          localStorage.removeItem(key);
          continue;
        }
        localStorage.setItem(key, String(value));
      }

      if (document.documentElement) {
        document.documentElement.dataset.qa = "1";
      }
    },
    {
      fixedNowIso: QA_FIXED_NOW_ISO,
      workspaceScenario,
      sceneLocalStorage: scene.localStorage ?? {},
    },
  );
}

async function installQaRoutes(page, scene) {
  const hostedScenario = createHostedScenario(scene.hostedMode);
  const workspaceScenario = createWorkspaceScenario(scene.workspaceMode);

  await page.route("**/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const pathname = normalizePathname(url.pathname);

    if (
      pathname.startsWith("/_app/") ||
      pathname.startsWith("/assets/") ||
      pathname === "/favicon.svg" ||
      pathname === "/apple-touch-icon.png" ||
      pathname === "/manifest.json" ||
      pathname === "/robots.txt"
    ) {
      await route.continue();
      return;
    }

    if (pathname.startsWith("/hosted/api/")) {
      const apiPath = normalizePathname(pathname.replace(/^\/hosted\/api/, ""));
      await handleHostedApiRoute(route, request, url, apiPath, hostedScenario);
      return;
    }

    await handleWorkspaceApiRoute(
      route,
      request,
      url,
      pathname,
      workspaceScenario,
    );
  });
}

async function handleHostedApiRoute(route, request, url, pathname, scenario) {
  if (pathname === "/account/me" && request.method() === "GET") {
    if (scenario.accountState === "pending") {
      await new Promise(() => {});
      return;
    }
    if (scenario.accountState !== "authed") {
      await route.fulfill(
        jsonResponse(401, { error: { message: "unauthorized" } }),
      );
      return;
    }
    await route.fulfill(jsonResponse(200, { account: QA_HOSTED_ACCOUNT }));
    return;
  }

  if (pathname === "/organizations" && request.method() === "GET") {
    await route.fulfill(
      jsonResponse(200, {
        organizations: sliceByLimit(scenario.organizations, url.searchParams),
        next_cursor: "",
      }),
    );
    return;
  }

  if (pathname === "/workspaces" && request.method() === "GET") {
    const orgId = String(url.searchParams.get("organization_id") ?? "").trim();
    const items = scenario.workspaces.filter((workspace) => {
      if (!orgId) {
        return true;
      }
      return String(workspace.organization_id) === orgId;
    });
    await route.fulfill(
      jsonResponse(200, {
        workspaces: sliceByLimit(items, url.searchParams),
        next_cursor: "",
      }),
    );
    return;
  }

  const billingMatch = pathname.match(/^\/organizations\/([^/]+)\/billing$/);
  if (billingMatch && request.method() === "GET") {
    const organizationId = billingMatch[1];
    if (organizationId !== QA_HOSTED_BILLING_SUMMARY.organization_id) {
      await route.fulfill(
        jsonResponse(404, { error: { message: "not found" } }),
      );
      return;
    }
    await route.fulfill(
      jsonResponse(200, { summary: QA_HOSTED_BILLING_SUMMARY }),
    );
    return;
  }

  const usageMatch = pathname.match(
    /^\/organizations\/([^/]+)\/usage-summary$/,
  );
  if (usageMatch && request.method() === "GET") {
    const organizationId = usageMatch[1];
    if (organizationId !== QA_HOSTED_BILLING_SUMMARY.organization_id) {
      await route.fulfill(
        jsonResponse(404, { error: { message: "not found" } }),
      );
      return;
    }
    await route.fulfill(
      jsonResponse(200, { summary: QA_HOSTED_BILLING_SUMMARY.usage_summary }),
    );
    return;
  }

  const membershipsMatch = pathname.match(
    /^\/organizations\/([^/]+)\/memberships$/,
  );
  if (membershipsMatch && request.method() === "GET") {
    await route.fulfill(
      jsonResponse(200, {
        memberships: [
          {
            id: "membership_owner_jordan",
            organization_id: membershipsMatch[1],
            role: "owner",
            status: "active",
            account_display_name: QA_HOSTED_ACCOUNT.display_name,
            account_email: QA_HOSTED_ACCOUNT.email,
          },
        ],
      }),
    );
    return;
  }

  const organizationMatch = pathname.match(/^\/organizations\/([^/]+)$/);
  if (organizationMatch && request.method() === "GET") {
    const organization = scenario.organizations.find(
      (item) => String(item.id) === organizationMatch[1],
    );
    if (!organization) {
      await route.fulfill(
        jsonResponse(404, { error: { message: "not found" } }),
      );
      return;
    }
    await route.fulfill(jsonResponse(200, { organization }));
    return;
  }

  const checkoutSessionMatch = pathname.match(
    /^\/billing\/checkout-session\/([^/]+)$/,
  );
  if (checkoutSessionMatch && request.method() === "GET") {
    await route.fulfill(
      jsonResponse(200, {
        organization_id: QA_HOSTED_BILLING_SUMMARY.organization_id,
      }),
    );
    return;
  }

  const mockCheckoutMatch = pathname.match(
    /^\/organizations\/([^/]+)\/billing\/mock-checkout-complete$/,
  );
  if (mockCheckoutMatch && request.method() === "POST") {
    await route.fulfill(jsonResponse(200, { ok: true }));
    return;
  }

  await route.fulfill(jsonResponse(404, { error: { message: "not found" } }));
}

async function handleWorkspaceApiRoute(
  route,
  request,
  url,
  pathname,
  scenario,
) {
  if (pathname === "/auth/session" && request.method() === "GET") {
    await route.fulfill(jsonResponse(200, { agent: QA_AUTH_AGENT }));
    return;
  }

  if (pathname === "/meta/handshake" && request.method() === "GET") {
    await route.fulfill(
      jsonResponse(200, {
        dev_actor_mode: false,
        human_auth_mode: "passkey",
      }),
    );
    return;
  }

  if (pathname === "/actors" && request.method() === "GET") {
    const items = filterByQuery(QA_ACTORS, url.searchParams.get("q"), [
      "id",
      "display_name",
      "tags",
    ]);
    await route.fulfill(
      jsonResponse(200, {
        actors: sliceByLimit(items, url.searchParams),
      }),
    );
    return;
  }

  if (pathname === "/auth/principals" && request.method() === "GET") {
    await route.fulfill(
      jsonResponse(200, {
        principals: sliceByLimit(QA_PRINCIPALS, url.searchParams),
        active_human_principal_count: 1,
        next_cursor: "",
      }),
    );
    return;
  }

  if (pathname === "/auth/invites" && request.method() === "GET") {
    await route.fulfill(
      jsonResponse(200, {
        invites: sliceByLimit(QA_INVITES, url.searchParams),
      }),
    );
    return;
  }

  if (pathname === "/auth/audit" && request.method() === "GET") {
    await route.fulfill(
      jsonResponse(200, {
        events: sliceByLimit(QA_AUTH_AUDIT, url.searchParams),
        next_cursor: "",
      }),
    );
    return;
  }

  if (pathname === "/secrets" && request.method() === "GET") {
    await route.fulfill(
      jsonResponse(200, {
        secrets: sliceByLimit(QA_SECRETS, url.searchParams),
      }),
    );
    return;
  }

  const revealSecretMatch = pathname.match(/^\/secrets\/([^/]+)\/reveal$/);
  if (revealSecretMatch && request.method() === "POST") {
    await route.fulfill(
      jsonResponse(200, {
        value: "sk-qa-visible-secret-value",
      }),
    );
    return;
  }

  if (pathname === "/inbox" && request.method() === "GET") {
    if (scenario.inboxState === "loading") {
      await new Promise(() => {});
      return;
    }
    if (scenario.inboxState === "error") {
      await route.fulfill(
        jsonResponse(500, { error: { message: "QA baseline inbox failure." } }),
      );
      return;
    }
    const items = scenario.inboxState === "populated" ? QA_INBOX_POPULATED : [];
    await route.fulfill(
      jsonResponse(200, {
        items,
        generated_at: QA_FIXED_NOW_ISO,
      }),
    );
    return;
  }

  const inboxItemMatch = pathname.match(/^\/inbox\/([^/]+)$/);
  if (inboxItemMatch && request.method() === "GET") {
    if (scenario.askState === "error") {
      await route.fulfill(
        jsonResponse(500, {
          error: { message: "QA baseline ask context failed." },
        }),
      );
      return;
    }
    if (inboxItemMatch[1] !== QA_ASK_ITEM.id) {
      await route.fulfill(
        jsonResponse(404, { error: { message: "not found" } }),
      );
      return;
    }
    await route.fulfill(jsonResponse(200, { item: QA_ASK_ITEM }));
    return;
  }

  if (pathname === "/topics" && request.method() === "GET") {
    const items = filterByQuery(QA_TOPICS, url.searchParams.get("q"), [
      "id",
      "thread_id",
      "title",
      "current_summary",
      "tags",
    ]);
    await route.fulfill(
      jsonResponse(200, {
        topics: sliceByLimit(items, url.searchParams),
      }),
    );
    return;
  }

  if (pathname === "/boards" && request.method() === "GET") {
    const items = filterByQuery(QA_BOARDS, url.searchParams.get("q"), [
      "id",
      "title",
      "labels",
      "owners",
    ]);
    const rows = qaBoardListRows(sliceByLimit(items, url.searchParams));
    await route.fulfill(
      jsonResponse(200, {
        boards: rows,
      }),
    );
    return;
  }

  if (pathname === "/docs" && request.method() === "GET") {
    const items = filterByQuery(QA_DOCUMENTS, url.searchParams.get("q"), [
      "id",
      "title",
      "labels",
    ]);
    await route.fulfill(
      jsonResponse(200, {
        documents: sliceByLimit(items, url.searchParams),
      }),
    );
    return;
  }

  if (pathname === "/artifacts" && request.method() === "GET") {
    const items = filterByQuery(QA_ARTIFACTS, url.searchParams.get("q"), [
      "id",
      "summary",
      "refs",
    ]);
    await route.fulfill(
      jsonResponse(200, {
        artifacts: sliceByLimit(items, url.searchParams),
      }),
    );
    return;
  }

  if (pathname === "/events" && request.method() === "GET") {
    await route.fulfill(
      jsonResponse(200, {
        events: sliceByLimit(QA_EVENTS, url.searchParams),
      }),
    );
    return;
  }

  await route.continue();
}

async function captureScene(browser, baseUrl, scene, outDir) {
  const context = await browser.newContext({
    viewport: DEFAULT_VIEWPORT,
    deviceScaleFactor: 2,
    colorScheme: "dark",
    reducedMotion: "reduce",
  });

  try {
    const page = await context.newPage();
    await installQaEnvironment(page, scene);
    await installQaRoutes(page, scene);
    await page.goto(buildSceneUrl(baseUrl, scene.path), {
      waitUntil: "domcontentloaded",
      timeout: 30_000,
    });
    await scene.waitFor(page);
    await page.waitForTimeout(150);
    await page.evaluate(async () => {
      if (document.fonts?.ready) {
        await document.fonts.ready;
      }
    });

    const outputPath = path.join(outDir, `${scene.name}.png`);
    await page.screenshot({
      path: outputPath,
      fullPage: false,
      animations: "disabled",
    });
    return { scene: scene.name, path: outputPath, status: "ok" };
  } finally {
    await context.close();
  }
}

async function captureAllScenes({ outDir, port }) {
  await ensureBuild();
  await rm(outDir, { recursive: true, force: true });
  await mkdir(outDir, { recursive: true });

  const core = await startMockCoreServer();
  const preview = await startBuiltUiServer(port);
  const browser = await chromium.launch({ headless: true });

  try {
    const results = [];
    for (const scene of QA_SCENES) {
      results.push(await captureScene(browser, preview.baseUrl, scene, outDir));
    }
    return results;
  } finally {
    await browser.close();
    await preview.stop();
    await core.stop();
  }
}

async function diffPngFiles(baselinePath, currentPath, diffPath) {
  const [baselineBuffer, currentBuffer] = await Promise.all([
    readFile(baselinePath),
    readFile(currentPath),
  ]);

  const baselinePng = PNG.sync.read(baselineBuffer);
  const currentPng = PNG.sync.read(currentBuffer);

  if (
    baselinePng.width !== currentPng.width ||
    baselinePng.height !== currentPng.height
  ) {
    return {
      mismatchPixels: Number.POSITIVE_INFINITY,
      mismatchRatio: Number.POSITIVE_INFINITY,
      dimensionMismatch: {
        baseline: `${baselinePng.width}x${baselinePng.height}`,
        current: `${currentPng.width}x${currentPng.height}`,
      },
    };
  }

  const diffPng = new PNG({
    width: baselinePng.width,
    height: baselinePng.height,
  });

  const mismatchPixels = pixelmatch(
    baselinePng.data,
    currentPng.data,
    diffPng.data,
    baselinePng.width,
    baselinePng.height,
    {
      threshold: 0.1,
      includeAA: false,
    },
  );

  const mismatchRatio =
    mismatchPixels / (baselinePng.width * baselinePng.height);

  if (mismatchPixels > 0) {
    await writeFile(diffPath, PNG.sync.write(diffPng));
  }

  return {
    mismatchPixels,
    mismatchRatio,
    dimensionMismatch: null,
  };
}

export async function runQaVisualCommand(options) {
  if (options.mode === "baseline") {
    const results = await captureAllScenes({
      outDir: options.outDir,
      port: options.port,
    });
    const summary = {
      mode: "baseline",
      outDir: options.outDir,
      fixedNow: QA_FIXED_NOW_ISO,
      scenes: results.map((result) => result.scene),
    };
    if (options.json) {
      console.log(JSON.stringify(summary, null, 2));
    } else {
      console.log(
        `Captured ${results.length} QA baseline screenshots into ${options.outDir}`,
      );
    }
    return 0;
  }

  await captureAllScenes({
    outDir: options.outDir,
    port: options.port,
  });

  await rm(QA_DIFF_DIR, { recursive: true, force: true });
  await mkdir(QA_DIFF_DIR, { recursive: true });

  const failures = [];
  const matches = [];

  for (const scene of QA_SCENES) {
    const baselinePath = path.join(QA_BASELINE_DIR, `${scene.name}.png`);
    const currentPath = path.join(options.outDir, `${scene.name}.png`);
    const diffPath = path.join(QA_DIFF_DIR, `${scene.name}.diff.png`);

    try {
      const result = await diffPngFiles(baselinePath, currentPath, diffPath);
      if (result.dimensionMismatch) {
        failures.push({
          scene: scene.name,
          reason: `dimension mismatch (${result.dimensionMismatch.baseline} vs ${result.dimensionMismatch.current})`,
        });
        continue;
      }

      const thresholdRatio =
        typeof scene.thresholdRatio === "number"
          ? scene.thresholdRatio
          : options.thresholdRatio;
      if (result.mismatchRatio > thresholdRatio) {
        failures.push({
          scene: scene.name,
          reason: `${(result.mismatchRatio * 100).toFixed(3)}% pixels differ (${result.mismatchPixels} px; threshold ${(thresholdRatio * 100).toFixed(3)}%)`,
        });
      } else {
        matches.push(scene.name);
      }
    } catch (error) {
      failures.push({
        scene: scene.name,
        reason: error instanceof Error ? error.message : String(error),
      });
    }
  }

  const summary = {
    mode: "diff",
    fixedNow: QA_FIXED_NOW_ISO,
    currentDir: options.outDir,
    baselineDir: QA_BASELINE_DIR,
    diffDir: QA_DIFF_DIR,
    thresholdRatio: options.thresholdRatio,
    ok: failures.length === 0,
    matches,
    failures,
  };

  if (options.json) {
    console.log(JSON.stringify(summary, null, 2));
  } else if (failures.length === 0) {
    console.log(
      `QA diff passed: ${matches.length}/${QA_SCENES.length} scenes within thresholds (default max diff ${(options.thresholdRatio * 100).toFixed(3)}%; some scenes allow more).`,
    );
  } else {
    console.error(
      `QA diff failed: ${failures.length}/${QA_SCENES.length} scenes exceeded ${(options.thresholdRatio * 100).toFixed(3)}%.`,
    );
    for (const failure of failures) {
      console.error(`  - ${failure.scene}: ${failure.reason}`);
    }
    console.error(`Diff images written to ${QA_DIFF_DIR}`);
  }

  return failures.length === 0 ? 0 : 1;
}

if (
  process.argv[1] &&
  fileURLToPath(import.meta.url) === path.resolve(process.argv[1])
) {
  runQaVisualCommand(parseCliArgs(process.argv))
    .then((code) => {
      process.exit(code);
    })
    .catch((error) => {
      console.error(
        "qa-visual failed:",
        error instanceof Error ? error.message : error,
      );
      process.exit(1);
    });
}
