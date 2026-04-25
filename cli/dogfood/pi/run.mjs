import fs from "node:fs";
import net from "node:net";
import path from "node:path";
import process from "node:process";
import { spawnSync, spawn } from "node:child_process";
import { fileURLToPath, pathToFileURL } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const packageRoot = here;
const repoRoot = path.resolve(packageRoot, "../../..");

export const scenarioConfigs = {
  "pilot-rescue": {
    roleLimit: 4,
    threadTitles: {
      main: "Pilot Rescue Sprint: NorthWave Launch Readiness",
      feedback: "Customer Escalation: NorthWave Pilot Feedback",
      delivery: "Delivery Plan: Pilot Fix + Rollout Sequencing",
    },
    documentId: "northwave-pilot-rescue-brief",
    documentTemplateContent:
      "# NorthWave Pilot Rescue Brief\n\nStatus: replace with recommended status\n\nFriday scope:\n- replace with scoped fixes\n\nDeferred follow-up:\n- replace with follow-up work\n\nLaunch recommendation:\n- replace with go/no-go call and rationale\n\nCustomer closure plan:\n- replace with exact deliverables to NorthWave and BriskPay\n",
    artifactIds: {
      feedbackMatrix: "artifact-feedback-matrix",
      feedbackQuotes: "artifact-feedback-quotes",
      launchChecklist: "artifact-launch-checklist",
      riskRegister: "artifact-risk-register",
      pilotMetrics: "artifact-pilot-metrics",
    },
    cardTitles: {
      digestFix: "Patch pilot digest cards to include card owner and due date",
      dedupeFix: "Stop duplicate escalation thread creation on status updates",
      closurePack: "Publish pilot rescue brief and customer closure plan",
    },
    roles: [
      {
        name: "support-lead",
        focus: "Translate customer pain into concrete launch requirements and closure conditions.",
        primaryThreadKey: "feedback",
        relatedThreadKeys: ["main"],
        artifactIds: ["feedbackMatrix", "feedbackQuotes"],
        cardTitles: [],
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
        cardTitles: ["digestFix", "dedupeFix"],
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
        cardTitles: ["digestFix", "dedupeFix", "closurePack"],
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
        cardTitles: ["closurePack"],
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
  "kids-lemonade-stand": {
    roleLimit: 3,
    threadTitles: {
      main: "Neighborhood Lemonade Stand Master Plan",
      sales: "Front Stand Sales, Smiles, and Sidewalk Pitches",
      backoffice: "Kitchen Prep, Lemon Squeezing, and Supply Stash",
    },
    boardTitle: "Saturday Lemonade Stand Mission Board",
    documentId: "kid-boss-lemonade-plan",
    documentIds: {
      mainPlan: "kid-boss-lemonade-plan",
      salesNotes: "kid-sales-pitch-notebook",
      prepLog: "kid-prep-notebook",
    },
    documentTemplateContent:
      "# Kid Boss Lemonade Plan\n\nMood:\n- replace with the cheerful vibe for the stand today\n\nSales game plan:\n- replace with the best sales ideas and sign tweaks\n\nKitchen and supply plan:\n- replace with the batch schedule and supply checks\n\nBoss notes:\n- replace with friendly bossy reminders and one helpful suggestion for each teammate\n\nEnd-of-day goal:\n- replace with the team's win condition for the day\n",
    documentTemplateContents: {
      mainPlan:
        "# Kid Boss Lemonade Plan\n\nMood:\n- replace with the cheerful vibe for the stand today\n\nMain plot:\n- replace with the big shared story for the afternoon rush, the rival stand rumor, and how the team stays playful\n\nSales game plan:\n- replace with the best sales ideas and sign tweaks\n\nKitchen and supply plan:\n- replace with the batch schedule, the ice plan, and cup backup plan\n\nBoss notes:\n- replace with friendly bossy reminders and one helpful suggestion for each teammate\n\nEnd-of-day goal:\n- replace with the team's win condition for the day\n",
      salesNotes:
        "# Ruby Pitch Notebook\n\nBest hook right now:\n- replace with the line that actually makes neighbors stop\n\nFunny sign experiments:\n- replace with the best joke and the rejected joke\n\nCustomer reactions:\n- replace with the patterns you are seeing from kids, parents, and the soccer crowd\n\nMessage to the team:\n- replace with one specific ask for Theo and one for Milo\n",
      prepLog:
        "# Theo Prep Log\n\nPitcher timing:\n- replace with the current batch plan and backup timing\n\nSupply watch:\n- replace with cup, ice, lemon, and sugar status\n\nKitchen drama:\n- replace with the one thing that could go chaotic if nobody pays attention\n\nMessage to the team:\n- replace with one specific ask for Ruby and one for Milo\n",
    },
    artifactIds: {
      scoreboard: "artifact-sales-scoreboard",
      signSlogans: "artifact-sign-slogans",
      prepChecklist: "artifact-prep-checklist",
      supplyList: "artifact-supply-stash",
      weatherNote: "artifact-weather-note",
      rivalRumor: "artifact-rival-stand-rumor",
      secretMenu: "artifact-secret-menu-sketch",
      neighborhoodFlyer: "artifact-neighborhood-flyer",
    },
    cardTitles: {
      signRefresh: "Refresh the chalkboard sign with giant prices and one excellent joke",
      rushPrep: "Get pitcher two icy cold before the soccer crowd stampede",
      secretMenu: "Decide whether the surprise mint special is genius or a terrible idea",
    },
    roles: [
      {
        name: "boss-kid",
        actorId: "actor-boss-kid",
        authUsername: "milo",
        focus: "Be the bossy but helpful kid running the stand: kick off the mission board, keep the plot moving, and make sure the team actually talks to each other instead of tossing notes into the void.",
        primaryThreadKey: "main",
        relatedThreadKeys: ["sales", "backoffice"],
        artifactIds: ["scoreboard", "prepChecklist", "supplyList", "rivalRumor", "secretMenu"],
        cardTitles: ["signRefresh", "rushPrep", "secretMenu"],
        primaryDocumentKey: "mainPlan",
        privateContext: [
          "You are Milo, the boss kid. You are a little bossy, but it should feel funny and useful, not mean.",
          "Your personal plot thread: prove that being organized is cool by creating the mission board early, getting the team to reply to each other in messages, and steering the stand through the afternoon rush without sounding like a tiny consultant.",
          "The big flexible plot: there might be a rival cookie table popping up nearby, the soccer crowd is coming later, and someone keeps floating a surprise mint special. Keep the team coordinated around those moving pieces without needing perfect timing.",
          "Create the shared board early, post a kickoff message, add at most one coordination card, and keep nudging the others to reply in-thread instead of only publishing formal updates.",
          "Update your plan doc once after the first round of messages, then refine it again before the final actor_statement if the plan changed.",
          "Before you publish the final actor_statement, wait until the other two roles have posted at least one message each, created or updated their own task cards, and updated their role documents.",
        ],
        deliverable: "Create the shared mission board, post a kickoff message on the main thread, add at most one coordination card, reply to at least one teammate message, update `kid-boss-lemonade-plan` during the run and refine it again if the plan changes, and then publish the final actor_statement with the day's plan and one helpful suggestion for each kid.",
        eventSummary: "Boss kid plan: today's lemonade stand game plan and helpful reminders",
        eventThreadKeys: ["main", "sales", "backoffice"],
        eventIncludeDocument: true,
        requireDocsUpdate: true,
      },
      {
        name: "sales-kid",
        actorId: "actor-sales-kid",
        authUsername: "ruby",
        focus: "Work the front stand like an enthusiastic neighborhood kid: greet people, notice what makes them smile, keep the chat lively in thread messages, and turn the sign into something people can actually read.",
        primaryThreadKey: "sales",
        relatedThreadKeys: ["main"],
        artifactIds: ["scoreboard", "signSlogans", "weatherNote", "neighborhoodFlyer", "secretMenu"],
        cardTitles: ["signRefresh", "secretMenu"],
        primaryDocumentKey: "salesNotes",
        privateContext: [
          "You are Ruby, the sales kid. Be playful, upbeat, and a little dramatic about making the stand look exciting.",
          "Your personal plot thread: figure out the best sign joke, test whether the surprise mint special helps or hurts, and collect enough real reactions to boss the sign into shape.",
          "Talk to the other kids in message_posted events, not just formal actor_statement notes. Ask Theo about timing and ask Milo to pick a final sign direction.",
          "Create or update one task card on the shared board that is clearly tied to the sales thread rather than the shared main thread.",
          "Update your notebook doc after you learn something useful, then refine it again near the end if the pitch or mint plan changed.",
          "You are helping a kids' lemonade stand have a good day, not writing a launch memo.",
        ],
        deliverable: "Post at least one main-thread message and one reply, create or update one sales task card on the shared board, update `kid-sales-pitch-notebook` during the run and refine it again if the pitch changes, and publish one actor_statement on the main thread with your best sales ideas, what customers seem to like, and one or two playful improvements for the sign or pitch.",
        eventSummary: "Sales kid update: sidewalk pitch ideas and what customers are liking",
        eventThreadKeys: ["main", "sales"],
        eventIncludeDocument: true,
        requireDocsUpdate: true,
      },
      {
        name: "backoffice-kid",
        actorId: "actor-backoffice-kid",
        authUsername: "theo",
        focus: "Handle kitchen prep and supplies like the organized kid behind the scenes: batches, lemons, sugar, cups, ice, and constant dramatic warnings about what happens if anyone ignores the cooler.",
        primaryThreadKey: "backoffice",
        relatedThreadKeys: ["main"],
        artifactIds: ["prepChecklist", "supplyList", "weatherNote", "rivalRumor", "secretMenu"],
        cardTitles: ["rushPrep", "secretMenu"],
        primaryDocumentKey: "prepLog",
        privateContext: [
          "You are Theo, the backoffice kid. Be practical, a tiny bit proud of your organized system, and still playful.",
          "Your personal plot thread: protect the ice, time the second batch correctly, and decide whether the surprise mint special is possible without causing sticky kitchen chaos.",
          "Talk to Ruby and Milo in thread messages. Ask Ruby when she expects the rush, and tell Milo what the real supply cliff is before cups or ice become a disaster.",
          "Create or update one task card on the shared board that is clearly tied to the prep thread rather than the shared main thread.",
          "Update your prep log after you decide the batch timing, then refine it again near the end if the supply or mint plan changed.",
          "Helpful output sounds like batch timing, restock warnings, prep shortcuts, and what the boss kid should stop forgetting.",
        ],
        deliverable: "Post at least one main-thread message and one reply, create or update one prep task card on the shared board, update `kid-prep-notebook` during the run and refine it again if the prep plan changes, and publish one actor_statement on the main thread with the prep plan, supply risks, and the most important behind-the-stand tasks for the next few hours.",
        eventSummary: "Backoffice kid update: prep timing, supply stash, and kitchen needs",
        eventThreadKeys: ["main", "backoffice"],
        eventIncludeDocument: true,
        requireDocsUpdate: true,
      },
    ],
  },
};

export function parseArgs(argv) {
  const options = {
    scenario: "pilot-rescue",
    chapter: "chapter-1",
    continueRun: "",
    provider: "zai",
    model: "glm-5",
    baseUrl: "",
    reportDir: path.join(repoRoot, "cli", ".tmp", "pi-dogfood"),
    apiKey: "",
    apiKeyFile: "",
    anxBin: "",
    coreBin: "",
    maxSeconds: 900,
    agentCount: 4,
    agentPrefix: "pi-dogfood-agent",
    agentStartStaggerSeconds: 20,
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
      case "--chapter":
        options.chapter = argv[++idx] ?? "";
        break;
      case "--continue-run":
        options.continueRun = argv[++idx] ?? "";
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
      case "--anx-bin":
        options.anxBin = argv[++idx] ?? "";
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
      case "--agent-start-stagger-seconds":
        options.agentStartStaggerSeconds = Number(argv[++idx] ?? "0");
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
  if (!options.chapter.trim()) {
    throw new Error("--chapter is required");
  }
  if (options.continueRun && options.baseUrl) {
    throw new Error("--continue-run cannot be combined with --base-url");
  }
  if (options.chapter !== "chapter-1" && !options.continueRun) {
    throw new Error(`chapter ${options.chapter} requires --continue-run so the scenario can continue existing state`);
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
  if (!Number.isFinite(options.agentStartStaggerSeconds) || options.agentStartStaggerSeconds < 0) {
    throw new Error("--agent-start-stagger-seconds must be zero or a positive number");
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

function resolveExistingPath(candidates) {
  for (const candidate of candidates) {
    if (!candidate) {
      continue;
    }
    if (fs.existsSync(candidate)) {
      return path.resolve(candidate);
    }
  }
  return "";
}

function renderScenario(content, baseUrl) {
  return content.replace(/`http:\/\/127\.0\.0\.1:8000`/g, `\`${baseUrl}\``);
}

function scenarioFilePath(scenario) {
  return path.join(packageRoot, "scenarios", `${scenario}.md`);
}

function scenarioChapterFilePath(scenario, chapter) {
  return path.join(packageRoot, "scenarios", scenario, `${chapter}.md`);
}

export function loadScenarioContent(scenario, chapter, baseUrl) {
  const basePath = scenarioFilePath(scenario);
  if (!fs.existsSync(basePath)) {
    throw new Error(`scenario file not found: ${basePath}`);
  }
  const baseMarkdown = renderScenario(fs.readFileSync(basePath, "utf8"), baseUrl);
  if (chapter === "chapter-1") {
    return {
      basePath,
      chapterPath: "",
      combinedMarkdown: baseMarkdown,
      chapterMarkdown: "",
    };
  }

  const chapterPath = scenarioChapterFilePath(scenario, chapter);
  if (!fs.existsSync(chapterPath)) {
    throw new Error(`chapter file not found: ${chapterPath}`);
  }
  const chapterMarkdown = renderScenario(fs.readFileSync(chapterPath, "utf8"), baseUrl);
  return {
    basePath,
    chapterPath,
    combinedMarkdown: `${baseMarkdown}\n\n---\n\n${chapterMarkdown}`,
    chapterMarkdown,
  };
}

function resolveContinueRunPath(continueRun, reportDir) {
  const trimmed = String(continueRun ?? "").trim();
  if (!trimmed) {
    return "";
  }
  const candidates = [
    path.isAbsolute(trimmed) ? trimmed : "",
    path.resolve(reportDir, trimmed),
    path.resolve(repoRoot, trimmed),
    path.resolve(packageRoot, trimmed),
  ];
  const resolved = resolveExistingPath(candidates);
  if (!resolved) {
    throw new Error(`continue run path not found: ${trimmed}`);
  }
  return resolved;
}

function loadPreviousRun(continueRun, reportDir, expectedScenario) {
  const resolved = resolveContinueRunPath(continueRun, reportDir);
  const stats = fs.statSync(resolved);
  const runDir = stats.isDirectory() ? resolved : path.dirname(resolved);
  const metadataPath = stats.isDirectory()
    ? path.join(resolved, "run-metadata.json")
    : resolved;
  if (!fs.existsSync(metadataPath)) {
    throw new Error(`run metadata not found at ${metadataPath}`);
  }
  const metadata = JSON.parse(fs.readFileSync(metadataPath, "utf8"));
  if (expectedScenario && metadata?.scenario !== expectedScenario) {
    throw new Error(`continue run scenario mismatch: expected ${expectedScenario}, got ${metadata?.scenario ?? "unknown"}`);
  }
  if (!metadata?.managed_core || !metadata?.core_workspace_dir) {
    throw new Error("continue run requires a previous managed-core Pi run with a persisted core workspace");
  }
  return {
    runDir,
    metadataPath,
    metadata,
    scenarioStatePath: path.join(runDir, "scenario-state.json"),
  };
}

function copyDirectory(sourceDir, targetDir) {
  fs.cpSync(sourceDir, targetDir, { recursive: true, force: true });
}

function hydrateContinuationHomes({ previousRunDir, runDir, agents }) {
  for (const agent of agents) {
    const sourceHome = agentHomeDir(previousRunDir, agent.agentId);
    if (!fs.existsSync(sourceHome)) {
      throw new Error(`missing prior agent home for ${agent.agentId}: ${sourceHome}`);
    }
    const targetHome = agentHomeDir(runDir, agent.agentId);
    ensureDir(path.dirname(targetHome));
    copyDirectory(sourceHome, targetHome);
    ensureDir(agent.workspaceDir);
  }
  return true;
}

function commandGuide(baseUrl, defaultUsername, { profileReady = false } = {}) {
  const authLines = profileReady
    ? [
        "- Show auth subcommands: `anx auth`",
        "- Verify the pre-registered default profile: `anx auth whoami`",
        "- List local profiles if needed: `anx auth list`",
        "- List taggable teammate handles directly: `anx auth principals list --taggable --handles-only`",
        "- Inspect linked principals when debugging Access or `@handle` behavior: `anx auth principals list --json`",
      ]
    : [
        "- Show auth subcommands: `anx auth`",
        `- Register default profile: \`anx auth register --username ${defaultUsername}\``,
        "- Verify current profile: `anx auth whoami`",
      ];
  return `# Agent Nexus command guide

Use these exact command shapes. Prefer them over guessing.

Base URL:
- ${baseUrl}

Auth:
${authLines.join("\n")}

Read workflow state:
- List threads: \`anx threads list\`
- Fast coordination read in one command: \`anx threads inspect --thread-id <thread-id>\`
- Canonical thread coordination read: \`anx threads workspace --thread-id <thread-id>\`
- Hydrated one-command coordination read: \`anx threads workspace --thread-id <thread-id> --include-related-event-content --include-artifact-content --verbose\`
- Focus recommendation review: \`anx threads recommendations --thread-id <thread-id>\`
- Full related recommendation content in one command: \`anx threads recommendations --thread-id <thread-id> --include-related-event-content --verbose\`
- Cross-thread aggregate context (optional): \`anx threads context --status active --type initiative --full-id\`
- Minimal backing thread record (optional): \`anx threads get --thread-id <thread-id>\` (same contract read as \`threads.inspect\`)
- List recent thread events: \`anx events list --thread-id <thread-id> --max-events 10 --full-id\`
- List recent thread messages only: \`anx events list --thread-id <thread-id> --type message_posted --max-events 10 --full-id\`
- Explain how visible thread messages should be authored: \`anx events explain message_posted\`
- List inbox items: \`anx inbox list\`
- List artifacts: \`anx artifacts list --thread-id <thread-id>\`
- Read artifact metadata: \`anx artifacts get --artifact-id <artifact-id>\`
- Read artifact content: \`anx artifacts content --artifact-id <artifact-id>\`
- List boards: \`anx boards list\`
- Read one board workspace: \`anx boards workspace --board-id <board-id>\`
- List cards: \`anx cards list\`
- Read one card: \`anx cards get --card-id <card-id>\`
- Read a seeded scenario document: \`anx docs get --document-id <document-id>\`
- Stage a document revision update: \`anx docs propose-update --document-id <document-id> --from-file doc-update-template.json\`
- Apply a staged document update: \`anx docs apply --proposal-id <proposal-id>\`
- Update a document immediately (no proposal): \`anx docs update --document-id <document-id> --from-file doc-update-template.json\`

Write workflow state:
- Topics are the primary mutable coordination resource; \`anx threads patch\`, \`anx threads apply\`, and other thread mutation commands are not supported.
- Create a visible thread message: \`anx events create --from-file message-template.json\`
- Reply to a message thread item: \`anx events create --from-file reply-template.json\`
- Update a topic in one step: \`anx topics patch --topic-id <topic-id> --from-file topic-patch.json\`
- Create a shared board from stdin: \`cat board-template.json | anx boards create --json\`
- Create a new card on a board from stdin: \`cat card-template.json | anx cards create --json\`
- For card-level changes, use \`anx cards patch --card-id <card-id> --from-file card-patch.json\` (see \`anx help cards patch\`).
- Validate an event before sending it: \`anx events validate --from-file event-template.json\`
- Dry-run an event create without sending it: \`anx events create --from-file event-template.json --dry-run\`
- Edit \`event-template.json\` in place, then create the event: \`anx events create --from-file event-template.json\`

Working event types for this scenario:
- \`message_posted\` for visible messages and replies
- \`actor_statement\` for your higher-signal role summary
`;
}

function resultTemplate() {
  return `# Result

## Summary

## anx commands attempted

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

async function apiJSONWithToken(baseUrl, apiPath, { method = "GET", accessToken = "", body = undefined, expectedStatuses = [200] } = {}) {
  const response = await fetch(`${baseUrl}${apiPath}`, {
    method,
    headers: {
      ...(accessToken ? { Authorization: `Bearer ${accessToken}` } : {}),
      ...(body !== undefined ? { "Content-Type": "application/json" } : {}),
    },
    ...(body !== undefined ? { body: JSON.stringify(body) } : {}),
  });
  if (!expectedStatuses.includes(response.status)) {
    throw new Error(`${method} ${apiPath} failed with status ${response.status}`);
  }
  return response.json();
}

function workspaceID() {
  return String(process.env.ANX_WORKSPACE_ID ?? "ws_main").trim() || "ws_main";
}

function agentProfilePath(homeDir, agentId) {
  return path.join(homeDir, ".config", "anx", "profiles", `${agentId}.json`);
}

function loadAgentProfile(homeDir, agentId) {
  const profilePath = agentProfilePath(homeDir, agentId);
  return JSON.parse(fs.readFileSync(profilePath, "utf8"));
}

async function ensureAgentWakeRegistration(baseUrl, anxBin, agent) {
  runAnxJSON({
    cwd: agent.workspaceDir,
    anxBin,
    baseUrl,
    homeDir: agent.homeDir,
    agentId: agent.agentId,
    args: ["auth", "whoami"],
  });
  const profile = loadAgentProfile(agent.homeDir, agent.agentId);
  const accessToken = String(profile?.access_token ?? "").trim();
  const actorID = String(agent.actorId ?? profile?.actor_id ?? "").trim();
  if (!accessToken || !actorID) {
    throw new Error(`missing auth state needed to register wake routing for ${agent.agentId}`);
  }
  await apiJSONWithToken(baseUrl, "/agents/me", {
    method: "PATCH",
    accessToken,
    body: {
      registration: {
        handle: agent.agentUsername,
        actor_id: actorID,
        status: "active",
        workspace_bindings: [
          {
            workspace_id: workspaceID(),
            enabled: true,
          },
        ],
      },
    },
    expectedStatuses: [200],
  });
}

async function ensureAgentWakeRegistrations(baseUrl, anxBin, agents) {
  for (const agent of agents) {
    await ensureAgentWakeRegistration(baseUrl, anxBin, agent);
  }
}

async function listEvents(baseUrl, { threadID = "", eventTypes = [] } = {}) {
  const params = new URLSearchParams();
  if (threadID) {
    params.set("thread_id", threadID);
  }
  for (const eventType of eventTypes) {
    params.append("type", eventType);
  }
  const suffix = params.toString() ? `?${params.toString()}` : "";
  return apiJSON(baseUrl, `/events${suffix}`);
}

export async function resolveSharedTargets(baseUrl, config) {
  const threadsResponse = await apiJSON(baseUrl, "/threads");
  const threads = Array.isArray(threadsResponse?.threads) ? threadsResponse.threads : [];
  const byTitle = Object.fromEntries(threads.map((thread) => [valueFrom(thread, "title", "summary"), thread]));

  const resolvedThreads = {};
  for (const [key, title] of Object.entries(config.threadTitles)) {
    const thread = byTitle[title];
    if (!thread?.id) {
      throw new Error(`failed to resolve scenario thread ${key}: ${title}`);
    }
    resolvedThreads[key] = thread;
  }

  const artifacts = {};
  for (const artifactId of Object.values(config.artifactIds)) {
    const response = await apiJSON(baseUrl, `/artifacts/${encodeURIComponent(artifactId)}`);
    artifacts[artifactId] = response?.artifact;
  }

  const cardsResponse = await apiJSON(baseUrl, "/cards");
  const allCards = Array.isArray(cardsResponse?.cards) ? cardsResponse.cards : [];
  const cardsByTitle = Object.fromEntries(allCards.map((card) => [valueFrom(card, "title", "summary"), card]));

  const inboxResponse = await apiJSON(baseUrl, "/inbox");
  const inboxItems = Array.isArray(inboxResponse?.items) ? inboxResponse.items : [];

  const documentIds = Object.entries(config.documentIds ?? {}).length > 0
    ? config.documentIds
    : { primary: config.documentId };
  const documents = {};
  for (const [key, documentId] of Object.entries(documentIds)) {
    documents[key] = {
      id: documentId,
      response: await apiJSON(baseUrl, `/docs/${encodeURIComponent(documentId)}`),
    };
  }

  const topicsResponse = await apiJSON(baseUrl, "/topics");
  const topics = Array.isArray(topicsResponse?.topics) ? topicsResponse.topics : [];
  const topicsByTitle = Object.fromEntries(topics.map((topic) => [valueFrom(topic, "title", "summary"), topic]));

  const resolvedTopics = {};
  for (const [key, title] of Object.entries(config.threadTitles)) {
    resolvedTopics[key] = topicsByTitle[title] ?? null;
  }

  const resolvedCards = { all: allCards };
  for (const [key, title] of Object.entries(config.cardTitles ?? {})) {
    resolvedCards[key] = cardsByTitle[title] ?? null;
  }

  return {
    threads: resolvedThreads,
    topics: resolvedTopics,
    artifacts,
    cards: resolvedCards,
    inboxItems,
    documents,
  };
}

async function collectScenarioState(baseUrl, config) {
  const sharedTargets = await resolveSharedTargets(baseUrl, config);
  const boardsResponse = await apiJSON(baseUrl, "/boards");
  const boardItems = Array.isArray(boardsResponse?.boards) ? boardsResponse.boards : [];
  const cardsResponse = await apiJSON(baseUrl, "/cards");
  const cards = Array.isArray(cardsResponse?.cards) ? cardsResponse.cards : [];
  const mainEventsResponse = await listEvents(baseUrl, {
    threadID: sharedTargets.threads.main?.id ?? "",
    eventTypes: ["message_posted"],
  });
  const mainMessages = Array.isArray(mainEventsResponse?.events) ? mainEventsResponse.events : [];

  const documents = {};
  for (const [key, documentEntry] of Object.entries(sharedTargets.documents ?? {})) {
    documents[key] = {
      id: documentEntry?.id ?? "",
      title: valueFrom(documentEntry?.response?.document, "title"),
      headRevisionID: valueFrom(documentEntry?.response?.revision, "revision_id"),
      headRevisionNumber: valueFrom(documentEntry?.response?.document, "head_revision_number"),
    };
  }

  return {
    generated_at: new Date().toISOString(),
    threads: Object.fromEntries(
      Object.entries(sharedTargets.threads ?? {}).map(([key, thread]) => [key, {
        id: thread?.id ?? "",
        title: valueFrom(thread, "title", "summary"),
      }]),
    ),
    topics: Object.fromEntries(
      Object.entries(sharedTargets.topics ?? {}).map(([key, topic]) => [key, {
        id: valueFrom(topic, "id"),
        title: valueFrom(topic, "title", "summary"),
      }]),
    ),
    boards: boardItems.map((item) => ({
      id: valueFrom(item?.board, "id"),
      title: valueFrom(item?.board, "title"),
      status: valueFrom(item?.board, "status"),
      cardCount: item?.summary?.card_count ?? 0,
      updatedAt: valueFrom(item?.board, "updated_at"),
    })),
    cards: cards.map((card) => ({
      id: valueFrom(card, "id"),
      title: valueFrom(card, "title", "summary"),
      boardID: valueFrom(card, "board_id"),
      columnKey: valueFrom(card, "column_key"),
      updatedAt: valueFrom(card, "updated_at"),
      assigneeRefs: Array.isArray(card?.assignee_refs) ? card.assignee_refs : [],
      relatedRefs: Array.isArray(card?.related_refs) ? card.related_refs : [],
      parentThreadID: valueFrom(card, "parent_thread_id"),
      threadID: valueFrom(card, "thread_id"),
    })),
    documents,
    recentMainMessages: mainMessages.slice(-12).map((event) => ({
      id: valueFrom(event, "id"),
      actorID: valueFrom(event, "actor_id"),
      summary: valueFrom(event, "summary"),
      text: valueFrom(event?.payload, "text"),
      refs: Array.isArray(event?.refs) ? event.refs : [],
    })),
  };
}

function chapterStateMarkdown(chapterState, previousRun) {
  const lines = [
    "# Chapter State",
    "",
    `Continuing from run: ${previousRun?.metadata?.run_id ?? "unknown"}`,
    `Scenario: ${previousRun?.metadata?.scenario ?? "unknown"}`,
    `State captured at: ${chapterState.generated_at}`,
    "",
    "Hard rule: continue the existing workspace state. Do not recreate documents, boards, cards, or identities that already exist.",
    "",
    "Threads:",
  ];

  for (const [key, thread] of Object.entries(chapterState.threads ?? {})) {
    lines.push(`- ${key}: ${thread.id} :: ${thread.title}`);
  }

  lines.push("", "Boards:");
  if ((chapterState.boards ?? []).length === 0) {
    lines.push("- none yet");
  } else {
    for (const board of chapterState.boards) {
      lines.push(`- ${board.id} :: ${board.title} :: status=${board.status} :: cards=${board.cardCount}`);
    }
  }

  lines.push("", "Cards:");
  if ((chapterState.cards ?? []).length === 0) {
    lines.push("- none yet");
  } else {
    for (const card of chapterState.cards) {
      lines.push(`- ${card.id} :: ${card.title} :: board=${card.boardID} :: column=${card.columnKey} :: parent_thread=${card.parentThreadID || "none"}`);
    }
  }

  lines.push("", "Documents:");
  for (const [key, documentState] of Object.entries(chapterState.documents ?? {})) {
    lines.push(`- ${key}: ${documentState.id} :: ${documentState.title} :: head revision ${documentState.headRevisionNumber} (${documentState.headRevisionID})`);
  }

  lines.push("", "Recent main-thread messages:");
  for (const message of chapterState.recentMainMessages ?? []) {
    lines.push(`- ${message.id} :: ${message.actorID} :: ${message.summary}`);
    if (message.text) {
      lines.push(`  ${message.text}`);
    }
  }

  return `${lines.join("\n")}\n`;
}

function roleTargets(config, shared, role) {
  const primaryThread = shared.threads[role.primaryThreadKey];
  const relatedThreads = role.relatedThreadKeys.map((key) => shared.threads[key]).filter(Boolean);
  const roleArtifacts = role.artifactIds.map((key) => shared.artifacts[config.artifactIds[key]]).filter(Boolean);
  const roleCards = role.cardTitles.map((key) => shared.cards[key]).filter(Boolean);
  const relevantThreadIds = new Set([primaryThread?.id, ...relatedThreads.map((thread) => thread.id)]);
  const relevantInboxItems = shared.inboxItems.filter((item) => relevantThreadIds.has(valueFrom(item, "thread_id", "threadId")));
  const documentsByKey = shared.documents ?? {};
  const primaryDocumentKey = role.primaryDocumentKey ?? "primary";
  const primaryDocument = documentsByKey[primaryDocumentKey] ?? null;
  const roleDocuments = Object.entries(documentsByKey)
    .filter(([key]) => key === primaryDocumentKey || (role.documentKeys ?? []).includes(key))
    .map(([, document]) => document)
    .filter(Boolean);
  return {
    mainThread: shared.threads.main,
    mainTopic: shared.topics?.main ?? null,
    primaryThread,
    relatedThreads,
    threadsByKey: shared.threads,
    topic: shared.topics?.[role.primaryThreadKey] ?? null,
    artifacts: roleArtifacts,
    cards: roleCards,
    inboxItems: relevantInboxItems,
    primaryDocument,
    roleDocuments,
    documentsByKey,
  };
}

function eventTemplate(role, targets) {
  const refs = [];
  for (const threadKey of role.eventThreadKeys ?? []) {
    const thread = targets.threadsByKey?.[threadKey];
    if (thread?.id) {
      refs.push(`thread:${thread.id}`);
    }
  }
  if (role.eventIncludeDocument) {
    if (targets.primaryDocument?.id) {
      refs.push(`document:${targets.primaryDocument.id}`);
    }
  }
  for (const artifact of targets.artifacts) {
    refs.push(`artifact:${artifact.id}`);
  }
  for (const card of targets.cards) {
    refs.push(`card:${card.id}`);
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
        "Replace with specific facts from the threads, artifacts, and cards you inspected."
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

function messageTemplateForThread(thread) {
  return `{
  "event": {
    "type": "message_posted",
    "thread_id": "${thread.id}",
    "thread_ref": "thread:${thread.id}",
    "refs": [
      "thread:${thread.id}"
    ],
    "summary": "Message: replace with a short kid-style subject line",
    "payload": {
      "text": "Replace this with the actual message you want your teammates to read."
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

function messageTemplate(role, targets) {
  return messageTemplateForThread(targets.mainThread);
}

function replyTemplateForThread(thread) {
  return `{
  "event": {
    "type": "message_posted",
    "thread_id": "${thread.id}",
    "thread_ref": "thread:${thread.id}",
    "refs": [
      "thread:${thread.id}",
      "event:REPLY_TO_EVENT_ID"
    ],
    "summary": "Message reply: replace with a short reply subject",
    "payload": {
      "text": "Replace this with a direct reply to one teammate message. Keep the event:REPLY_TO_EVENT_ID ref updated."
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

function replyTemplate(role, targets) {
  return replyTemplateForThread(targets.mainThread);
}

function boardTemplate(config, role, targets) {
  const documentRefs = targets.primaryDocument?.id
    ? [`document:${targets.primaryDocument.id}`]
    : [];
  const pinnedRefs = targets.artifacts.map((artifact) => `artifact:${artifact.id}`);
  const primaryTopicRef = targets.mainTopic?.id
    ? `topic:${targets.mainTopic.id}`
    : "";
  return `{
  "board": {
    "title": ${JSON.stringify(config.boardTitle ?? "Scenario Mission Board")},
    "status": "active",
    "document_refs": ${JSON.stringify(documentRefs, null, 4)},
    "pinned_refs": ${JSON.stringify(pinnedRefs, null, 4)},
    ${primaryTopicRef ? `"primary_topic_ref": ${JSON.stringify(primaryTopicRef)},` : ""}
    "provenance": {
      "sources": [
        "inferred"
      ],
      "notes": "Created during the Pi dogfood scenario to track the stand plan."
    }
  }
}
`;
}

export function cardTemplate(role, targets, { boardID = "" } = {}) {
  // Board cards are effectively one-active-card-per-parent-thread on a board.
  // Keep each role's default card anchored to that role's primary thread instead
  // of attaching every card to the shared main thread.
  const parentThread = targets.primaryThread ?? targets.mainThread;
  const relatedRefs = [
    `thread:${parentThread.id}`,
    ...(targets.topic?.id ? [`topic:${targets.topic.id}`] : []),
    ...targets.artifacts.slice(0, 2).map((artifact) => `artifact:${artifact.id}`),
  ];
  return `{
  "board_id": ${JSON.stringify(boardID || "REPLACE_WITH_BOARD_ID")},
  "card": {
    "title": "Replace with a concrete stand task",
    "summary": "Replace with the actual task this kid is taking on.",
    "column_key": "ready",
    "risk": "medium",
    "assignee_refs": [],
    "related_refs": ${JSON.stringify(relatedRefs, null, 4)},
    "resolution_refs": [],
    ${targets.topic?.id ? `"topic_ref": ${JSON.stringify(`topic:${targets.topic.id}`)},` : ""}
    "provenance": {
      "sources": [
        "inferred"
      ],
      "notes": "Created during the Pi dogfood scenario."
    }
  }
}
`;
}

export function roleCardState(role, targets, chapterState) {
  const cards = Array.isArray(chapterState?.cards) ? chapterState.cards : [];
  if (cards.length === 0) {
    return null;
  }
  const preferredParentThreadID = targets.primaryThread?.id ?? targets.mainThread?.id ?? "";
  if (!preferredParentThreadID) {
    return null;
  }

  const directMatch = cards.find((card) => card?.parentThreadID === preferredParentThreadID);
  if (directMatch) {
    return directMatch;
  }

  const actorRef = role.actorId ? `actor:${role.actorId}` : "";
  if (actorRef) {
    const assigneeMatch = cards.find((card) => Array.isArray(card?.assigneeRefs) && card.assigneeRefs.includes(actorRef));
    if (assigneeMatch) {
      return assigneeMatch;
    }
  }

  return null;
}

export function cardPatchTemplate(role, targets, cardState) {
  const relatedRefs = [
    `thread:${targets.primaryThread?.id ?? targets.mainThread.id}`,
    ...(targets.topic?.id ? [`topic:${targets.topic.id}`] : []),
    ...targets.artifacts.slice(0, 2).map((artifact) => `artifact:${artifact.id}`),
  ];
  const assigneeRefs = role.actorId ? [`actor:${role.actorId}`] : [];
  return `{
  "if_updated_at": ${JSON.stringify(cardState?.updatedAt ?? "")},
  "patch": {
    "summary": "Replace with the updated task status, next move, or blocker for this kid.",
    "assignee_refs": ${JSON.stringify(assigneeRefs, null, 4)},
    "related_refs": ${JSON.stringify(relatedRefs, null, 4)},
    ${targets.topic?.id ? `"topic_ref": ${JSON.stringify(`topic:${targets.topic.id}`)},` : ""}
    "provenance": {
      "sources": [
        "inferred"
      ],
      "notes": "Updated during a continued Pi dogfood scenario chapter."
    }
  }
}
`;
}

function docUpdateTemplate(targets, config, role) {
  const primaryDocument = targets.primaryDocument;
  const primaryDocumentKey = role.primaryDocumentKey ?? "primary";
  const templateContent =
    config.documentTemplateContents?.[primaryDocumentKey] ??
    config.documentTemplateContent;
  const headRevision = valueFrom(primaryDocument?.response?.revision, "revision_id");
  const artifactRefs = targets.artifacts
    .map((artifact) => artifact?.id)
    .filter(Boolean)
    .map((artifactId) => `artifact:${artifactId}`);
  const refs = [...new Set([
    `thread:${targets.mainThread.id}`,
    ...(primaryDocument?.id ? [`document:${primaryDocument.id}`] : []),
    ...artifactRefs,
  ])];
  return `{
  "if_base_revision": "${headRevision}",
  "refs": ${JSON.stringify(refs, null, 4)},
  "content_type": "text",
  "content": ${JSON.stringify(templateContent)}
}
`;
}

function targetsGuide(role, targets) {
  const lines = [
    "# Scenario Targets",
    "",
    "Use these resolved IDs directly. Do not spend turns rediscovering them.",
    "",
    "Prefer topic workspace, cards, and boards for coordination; thread commands below are diagnostic (backing-thread tooling).",
    "Use `message_posted` events for in-thread chat and replies; do not rely on actor_statement alone if the scenario asks for live coordination.",
    "",
    `Shared goal thread: ${targets.mainThread.id}`,
    `Shared goal title: ${targets.mainThread.title}`,
    `Primary thread for your role: ${targets.primaryThread.id}`,
    `Primary thread title: ${targets.primaryThread.title}`,
  ];
  if (targets.topic?.id) {
    lines.push(
      `Topic workspace (primary read for this role): anx topics workspace --topic-id ${targets.topic.id}`,
      `Topic record: anx topics get --topic-id ${targets.topic.id}`,
    );
  }
  lines.push(
    `Canonical read shared goal thread: anx threads workspace --thread-id ${targets.mainThread.id}`,
    `Hydrated read shared goal thread: anx threads workspace --thread-id ${targets.mainThread.id} --include-related-event-content --include-artifact-content --verbose`,
    `Canonical read your primary thread: anx threads workspace --thread-id ${targets.primaryThread.id}`,
    `Recommendation review shared goal thread: anx threads recommendations --thread-id ${targets.mainThread.id}`,
    `Optional minimal thread record shared goal thread: anx threads get --thread-id ${targets.mainThread.id}`,
    `Optional minimal thread record your primary thread: anx threads get --thread-id ${targets.primaryThread.id}`,
  );

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
      lines.push(`  metadata: anx artifacts get --artifact-id ${artifact.id}`);
      lines.push(`  content: anx artifacts content --artifact-id ${artifact.id}`);
    }
  }

  if (targets.cards.length > 0) {
    lines.push("", "Cards in scope:");
    for (const card of targets.cards) {
      lines.push(`- ${card.id} :: ${valueFrom(card, "title", "summary")}`);
      lines.push(`  detail: anx cards get --card-id ${card.id}`);
    }
  }

  if (targets.roleDocuments.length > 0) {
    lines.push("", "Documents in scope:");
    for (const document of targets.roleDocuments) {
      lines.push(`- ${document.id}`);
      lines.push(`  read: anx docs get --document-id ${document.id}`);
      lines.push(`  content: anx docs content --document-id ${document.id}`);
    }
  }

  if (targets.inboxItems.length > 0) {
    lines.push("", "Relevant inbox items:");
    for (const item of targets.inboxItems) {
      lines.push(`- ${valueFrom(item, "id")} :: ${valueFrom(item, "category", "kind", "type")} :: ${valueFrom(item, "title", "summary")}`);
    }
  }

  lines.push("", `Your deliverable: ${role.deliverable}`);
  lines.push(
    "Message coordination checklist:",
    `- kickoff messages live on the main thread ${targets.mainThread.id}`,
    `- list current visible messages with: anx events list --thread-id ${targets.mainThread.id} --type message_posted --max-events 10 --full-id`,
    "- reply by adding `event:<message_event_id>` to `reply-template.json` refs before creating it",
  );
  if (role.requireDocsUpdate) {
    lines.push(
      `Primary document to update: ${targets.primaryDocument?.id ?? ""}`,
      `Read it first: anx docs get --document-id ${targets.primaryDocument?.id ?? ""}`,
      `Stage it: anx docs propose-update --document-id ${targets.primaryDocument?.id ?? ""} --from-file doc-update-template.json`,
      `Then apply it: anx docs apply --proposal-id <proposal-id>`,
      `Or write immediately: anx docs update --document-id ${targets.primaryDocument?.id ?? ""} --from-file doc-update-template.json`,
    );
  }
  lines.push(
    "",
    "Board/task checklist:",
    "- the boss kid should create the shared board early from `board-template.json`",
    "- create or update one role-specific task card from `card-template.json` after the board exists",
    "- keep your card tied to your primary role thread instead of forcing every card onto the shared main thread",
    "- if you want multiple cards on one board, they need different parent-thread context; do not mindlessly clone the same thread refs",
  );

  return `${lines.join("\n")}\n`;
}

function privateContextGuide(role) {
  const lines = [
    "# Role Context",
    "",
    `Role: ${role.name}`,
    `Focus: ${role.focus}`,
    role.authUsername ? `Preferred @handle: @${role.authUsername}` : "",
    role.actorId ? `Seeded actor id: ${role.actorId}` : "",
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

function buildAnxBinary(runDir, providedPath) {
  return buildGoBinary(runDir, providedPath, "cli", "./cmd/anx", "anx");
}

function buildCoreBinary(runDir, providedPath) {
  return buildGoBinary(runDir, providedPath, "core", "./cmd/anx-core", "anx-core");
}

function agentHomeDir(runDir, agentId) {
  return path.join(runDir, `home-${agentId}`);
}

function anxProcessEnv({ homeDir, anxBin, baseUrl }) {
  return {
    ...process.env,
    HOME: homeDir,
    XDG_CONFIG_HOME: path.join(homeDir, ".config"),
    PATH: `${path.dirname(anxBin)}${path.delimiter}${process.env.PATH ?? ""}`,
    ANX_BASE_URL: baseUrl,
  };
}

function runAnxJSON({ cwd, anxBin, baseUrl, homeDir, agentId, args }) {
  const result = spawnSync(
    anxBin,
    ["--json", "--base-url", baseUrl, "--agent", agentId, ...args],
    {
      cwd,
      env: anxProcessEnv({ homeDir, anxBin, baseUrl }),
      encoding: "utf8",
    },
  );
  if (result.error) {
    const detail = result.error instanceof Error ? result.error.message : String(result.error);
    throw new Error(`anx ${args.join(" ")} failed for ${agentId}: ${detail}`);
  }
  if (result.status !== 0) {
    const stderr = String(result.stderr ?? "").trim();
    const stdout = String(result.stdout ?? "").trim();
    throw new Error(
      `anx ${args.join(" ")} failed for ${agentId}: ${stderr || stdout || `exit ${result.status}`}`,
    );
  }

  let payload;
  try {
    payload = JSON.parse(String(result.stdout ?? ""));
  } catch (error) {
    const detail = error instanceof Error ? error.message : String(error);
    throw new Error(`failed to parse anx JSON output for ${agentId}: ${detail}`);
  }

  if (!payload?.ok) {
    const errorMessage =
      payload?.error?.message ??
      payload?.error?.code ??
      "anx command returned ok=false";
    throw new Error(`anx ${args.join(" ")} failed for ${agentId}: ${errorMessage}`);
  }
  return payload;
}

function prepareAgentProfiles({ anxBin, baseUrl, bootstrapToken, agents }) {
  if (!bootstrapToken) {
    return false;
  }
  if (agents.length === 0) {
    return true;
  }

  for (const agent of agents) {
    ensureDir(agent.homeDir);
    ensureDir(agent.workspaceDir);
  }

  const [issuer, ...invitees] = agents;
  runAnxJSON({
    cwd: issuer.workspaceDir,
    anxBin,
    baseUrl,
    homeDir: issuer.homeDir,
    agentId: issuer.agentId,
    args: [
      "auth", "register",
      "--username", issuer.agentUsername,
      "--bootstrap-token", bootstrapToken,
      ...(issuer.actorId ? ["--existing-actor-id", issuer.actorId] : []),
    ],
  });

  for (const invitee of invitees) {
    const invitePayload = runAnxJSON({
      cwd: issuer.workspaceDir,
      anxBin,
      baseUrl,
      homeDir: issuer.homeDir,
      agentId: issuer.agentId,
      args: ["auth", "invites", "create", "--kind", "agent"],
    });
    const inviteToken = String(invitePayload?.data?.token ?? "").trim();
    if (!inviteToken) {
      throw new Error(`invite creation returned no token for ${invitee.agentId}`);
    }
    runAnxJSON({
      cwd: invitee.workspaceDir,
      anxBin,
      baseUrl,
      homeDir: invitee.homeDir,
      agentId: invitee.agentId,
      args: [
        "auth", "register",
        "--username", invitee.agentUsername,
        "--invite-token", inviteToken,
        ...(invitee.actorId ? ["--existing-actor-id", invitee.actorId] : []),
      ],
    });
  }

  return true;
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
      const response = await fetch(`${baseUrl}/readyz`);
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

function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function seedCore(baseUrl, scenario) {
  const seedScript = path.join(packageRoot, "seed", "seed-core.mjs");
  const result = spawnSync("node", [seedScript], {
    cwd: repoRoot,
    stdio: "inherit",
    env: {
      ...process.env,
      ANX_CORE_BASE_URL: baseUrl,
      ANX_FORCE_SEED: "1",
      ANX_PI_SCENARIO: scenario,
    },
  });
  if (result.status !== 0) {
    throw new Error("failed to seed core from CLI-owned mock data");
  }
}

async function startManagedCore(runDir, coreBin, requestedBaseUrl, scenario, continuation = null) {
  if (requestedBaseUrl) {
    await waitForCore(requestedBaseUrl, 20000);
    return {
      baseUrl: requestedBaseUrl,
      bootstrapToken: String(process.env.ANX_BOOTSTRAP_TOKEN ?? "").trim(),
      stop: async () => {},
      managed: false,
      workspaceDir: "",
      logPath: "",
    };
  }

  const workspaceDir = continuation?.metadata?.core_workspace_dir
    ? path.resolve(continuation.metadata.core_workspace_dir)
    : path.join(runDir, "core-workspace");
  const logPath = path.join(runDir, "core.log");
  const schemaPath = path.join(repoRoot, "contracts", "anx-schema.yaml");
  const host = "127.0.0.1";
  const port = await findFreePort();
  const baseUrl = `http://${host}:${port}`;
  const bootstrapToken = continuation ? "" : `pi-bs-${runToken()}`;
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
    env: {
      ...process.env,
      ANX_DEV_REGISTER_LINKED_ACTORS: "1",
      ...(bootstrapToken ? { ANX_BOOTSTRAP_TOKEN: bootstrapToken } : {}),
    },
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
  if (!continuation) {
    await seedCore(baseUrl, scenario);
  }

  return {
    baseUrl,
    bootstrapToken,
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

function uniqueNonEmpty(values) {
  return [...new Set(values.map((value) => String(value).trim()).filter(Boolean))];
}

function collectErrorMessages(value, messages = []) {
  if (Array.isArray(value)) {
    for (const item of value) {
      collectErrorMessages(item, messages);
    }
    return messages;
  }
  if (!value || typeof value !== "object") {
    return messages;
  }
  for (const [key, nested] of Object.entries(value)) {
    if (key === "errorMessage" && typeof nested === "string") {
      messages.push(nested);
      continue;
    }
    collectErrorMessages(nested, messages);
  }
  if (value.stopReason === "error" && typeof value.errorMessage !== "string") {
    const scope = typeof value.type === "string" ? value.type : "pi event";
    messages.push(`${scope} reported stopReason=error without an errorMessage`);
  }
  return messages;
}

export function analyzePiEventLog(rawContent) {
  const parseErrors = [];
  const runtimeErrors = [];
  const lines = String(rawContent).split(/\r?\n/).filter(Boolean);

  for (const [index, line] of lines.entries()) {
    try {
      const parsed = JSON.parse(line);
      runtimeErrors.push(...collectErrorMessages(parsed));
    } catch (error) {
      const detail = error instanceof Error ? error.message : String(error);
      parseErrors.push(`line ${index + 1}: ${detail}`);
    }
  }

  return {
    parseErrors: uniqueNonEmpty(parseErrors),
    runtimeErrors: uniqueNonEmpty(runtimeErrors),
  };
}

export function validateAgentOutputs({ eventsPath, resultPath }) {
  const failures = [];
  if (!fs.existsSync(eventsPath)) {
    failures.push(`missing events log at ${eventsPath}`);
  } else {
    const diagnostics = analyzePiEventLog(fs.readFileSync(eventsPath, "utf8"));
    if (diagnostics.parseErrors.length > 0) {
      failures.push(`events log parse errors: ${diagnostics.parseErrors.join("; ")}`);
    }
    if (diagnostics.runtimeErrors.length > 0) {
      failures.push(`pi runtime errors: ${diagnostics.runtimeErrors.join("; ")}`);
    }
  }
  if (!fs.existsSync(resultPath)) {
    failures.push(`required artifact missing: ${path.basename(resultPath)}`);
  }
  return failures;
}

async function runPiAgent({
  runDir,
  piHomeDir,
  anxBin,
  coreBaseUrl,
  provider,
  model,
  apiKey,
  maxSeconds,
  agentId,
  agentCount,
  agentUsername,
  homeDir,
  profileReady,
  scenarioMarkdown,
  chapterID,
  chapterMarkdown = "",
  chapterState = null,
  chapterStateGuide = "",
  scenarioConfig,
  role,
  targets,
}) {
  const workspaceDir = agentWorkspaceDir(runDir, agentCount, agentId);
  const eventsPath = agentEventsPath(runDir, agentCount, agentId);
  const resultPath = agentResultPath(workspaceDir);
  ensureDir(workspaceDir);
  const authSetupLine = profileReady
    ? `- The temp workspace already has a local auth profile for username \`${agentUsername}\`. Verify it with \`anx auth whoami\`.`
    : `- Register with username \`${agentUsername}\` if no local profile exists. Start with \`anx auth whoami\` to inspect the current state.`;
  const authPromptLine = profileReady
    ? `A local auth profile for username ${agentUsername} is already registered; verify it with \`anx auth whoami\` instead of creating a new registration.`
    : `Use \`anx auth whoami\` to inspect auth state first. If no profile exists, register with username ${agentUsername} using the command guidance in COMMANDS.md.`;
  const continuationFiles = [];
  if (chapterMarkdown) {
    continuationFiles.push("- Chapter brief: ./CHAPTER.md");
  }
  if (chapterStateGuide) {
    continuationFiles.push("- Chapter state: ./CHAPTER_STATE.md");
  }
  const knownBoardID = chapterState?.boards?.[0]?.id ?? "";
  const existingRoleCard = roleCardState(role, targets, chapterState);
  const primaryThreadHasOwnTemplates =
    targets.primaryThread?.id &&
    targets.mainThread?.id &&
    targets.primaryThread.id !== targets.mainThread.id;

  const agentsContent = `# Pi Dogfood Run

You are evaluating the ANX CLI, not editing the repository.

Rules:
- Use the \`anx\` binary on PATH for all ANX interactions.
- Do not use \`curl\` for ANX API calls.
- Do not edit repository source files.
- Keep notes and artifacts inside the current working directory.
- This scenario lane is fixed to \`--provider zai --model glm-5\`. Do not change provider/model when validating the scenario.
- If you need to debug another provider or model, do it outside this scenario runner and label it separately from scenario results.
${authSetupLine}
- Before finishing, write \`result.md\` containing:
  - summary
  - anx commands attempted
  - friction
  - concrete suggestions

Agent role:
- Agent id: ${agentId}
- Role: ${role.name}
- Focus: ${role.focus}

Environment:
- Agent Nexus base URL: ${coreBaseUrl}
- Working directory: ${workspaceDir}
- Scenario brief: ./SCENARIO.md
- Chapter id: ${chapterID}
- Command guide: ./COMMANDS.md
- Scenario targets: ./TARGETS.md
- Role context: ./ROLE_CONTEXT.md
- Message template for the main thread: ./message-template.json
- Reply template for the main thread: ./reply-template.json
- Event template: ./event-template.json
- Board template: ./board-template.json
- Card template: ./card-template.json
${existingRoleCard ? `- Existing role card: ${existingRoleCard.id} (update template: ./card-patch-template.json)\n` : ""}
${primaryThreadHasOwnTemplates ? "- Primary-thread message template: ./primary-thread-message-template.json\n- Primary-thread reply template: ./primary-thread-reply-template.json" : ""}
${continuationFiles.join("\n")}
- Document update template (if present): ./doc-update-template.json
- Result template: ./result-template.md
`;
  writeFile(path.join(workspaceDir, "AGENTS.md"), agentsContent);
  writeFile(path.join(workspaceDir, "SCENARIO.md"), scenarioMarkdown);
  if (chapterMarkdown) {
    writeFile(path.join(workspaceDir, "CHAPTER.md"), chapterMarkdown);
  }
  if (chapterStateGuide) {
    writeFile(path.join(workspaceDir, "CHAPTER_STATE.md"), chapterStateGuide);
  }
  writeFile(
    path.join(workspaceDir, "COMMANDS.md"),
    commandGuide(coreBaseUrl, agentUsername, { profileReady }),
  );
  writeFile(path.join(workspaceDir, "TARGETS.md"), targetsGuide(role, targets));
  writeFile(path.join(workspaceDir, "ROLE_CONTEXT.md"), privateContextGuide(role));
  writeFile(path.join(workspaceDir, "message-template.json"), messageTemplate(role, targets));
  writeFile(path.join(workspaceDir, "reply-template.json"), replyTemplate(role, targets));
  if (primaryThreadHasOwnTemplates) {
    writeFile(
      path.join(workspaceDir, "primary-thread-message-template.json"),
      messageTemplateForThread(targets.primaryThread),
    );
    writeFile(
      path.join(workspaceDir, "primary-thread-reply-template.json"),
      replyTemplateForThread(targets.primaryThread),
    );
  }
  writeFile(path.join(workspaceDir, "event-template.json"), eventTemplate(role, targets));
  writeFile(path.join(workspaceDir, "board-template.json"), boardTemplate(scenarioConfig, role, targets));
  writeFile(path.join(workspaceDir, "card-template.json"), cardTemplate(role, targets, { boardID: knownBoardID }));
  if (existingRoleCard) {
    writeFile(path.join(workspaceDir, "card-patch-template.json"), cardPatchTemplate(role, targets, existingRoleCard));
  }
  if (role.requireDocsUpdate) {
    writeFile(path.join(workspaceDir, "doc-update-template.json"), docUpdateTemplate(targets, scenarioConfig, role));
  }
  writeFile(path.join(workspaceDir, "result-template.md"), resultTemplate());

  const continuationPrompt = chapterStateGuide
    ? `Read CHAPTER.md and CHAPTER_STATE.md first. Continue the existing scenario state from ${chapterID}. Do not recreate the board, docs, cards, or identities that already exist unless the chapter explicitly tells you to do so. Prefer adding new messages, new replies, card moves or updates, and new document revisions over creating duplicate resources.`
    : "";
  const prompt = role.requireDocsUpdate
    ? `Read SCENARIO.md, COMMANDS.md, TARGETS.md, and ROLE_CONTEXT.md. ${continuationPrompt} Execute your role with the real anx CLI. Use message-template.json for a kickoff message on the main thread, use reply-template.json for at least one reply to another kid's message, and use message_posted rather than actor_statement for those conversational updates.${primaryThreadHasOwnTemplates ? " Also use primary-thread-message-template.json for at least one role-thread update when the chapter asks for richer thread activity." : ""} If you are the boss kid, create the shared board early from board-template.json only if it does not already exist, add at most one coordination card from card-template.json if you do not already own one, and make the board visible to the others. ${existingRoleCard ? `If your role card ${existingRoleCard.id} already exists, update it with card-patch-template.json via \`anx cards patch --card-id ${existingRoleCard.id} --from-file card-patch-template.json\` instead of creating a duplicate.` : "If you need a new card, keep it tied to its intended role thread instead of attaching everything to the shared main thread."} Update doc-update-template.json in place, stage the document update with \`anx docs propose-update\`, inspect the diff, apply it with \`anx docs apply\`, then post your final actor_statement from event-template.json. Use \`anx events list --thread-id ${targets.mainThread.id} --type message_posted --max-events 10 --full-id\` to find reply targets. Write result.md and then give a short final summary.`
    : `Read SCENARIO.md, COMMANDS.md, TARGETS.md, and ROLE_CONTEXT.md. ${continuationPrompt} Execute your role with the real anx CLI. Use message-template.json for a kickoff message on the main thread, use reply-template.json for at least one reply to another kid's message, and create or inspect board work when the scenario asks for it.${primaryThreadHasOwnTemplates ? " Also use primary-thread-message-template.json for at least one role-thread update when the chapter asks for richer thread activity." : ""} ${existingRoleCard ? `Your role card ${existingRoleCard.id} already exists, so update it with card-patch-template.json via \`anx cards patch --card-id ${existingRoleCard.id} --from-file card-patch-template.json\` instead of creating a duplicate.` : "Create one role-specific task card from card-template.json after the board exists, and keep that card tied to your primary role thread rather than the shared main thread."} Use \`anx events list --thread-id ${targets.mainThread.id} --type message_posted --max-events 10 --full-id\` to find reply targets. After the conversational work is done, edit event-template.json in place, create the final actor_statement from that file, write result.md, and then give a short final summary.`;

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
    `Use anx on PATH for Agent Nexus (anx-core) interactions. Do not use curl. Work only inside the current directory. ${authPromptLine}`,
    "@SCENARIO.md",
    prompt,
  ];

  const eventStream = fs.createWriteStream(eventsPath, { flags: "a" });
  ensureDir(homeDir);
  const env = {
    ...anxProcessEnv({ homeDir, anxBin, baseUrl: coreBaseUrl }),
    PI_CODING_AGENT_DIR: path.join(piHomeDir, agentId),
  };
  ensureDir(env.PI_CODING_AGENT_DIR);

  return new Promise((resolve, reject) => {
    const settle = (finalizer) => {
      eventStream.end(() => {
        try {
          finalizer();
        } catch (error) {
          reject(error);
        }
      });
    };
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
      settle(() => reject(error));
    });
    child.on("exit", (code, signal) => {
      clearTimeout(timeout);
      if (signal) {
        settle(() => reject(new Error(`${agentId}: pi terminated by signal ${signal}`)));
        return;
      }
      if (code !== 0) {
        settle(() => reject(new Error(`${agentId}: pi exited with code ${code}`)));
        return;
      }
      settle(() => {
        const failures = validateAgentOutputs({ eventsPath, resultPath });
        if (failures.length > 0) {
          reject(new Error(`${agentId}: ${failures.join("; ")}`));
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
  });
}

async function main() {
  const options = parseArgs(process.argv.slice(2));
  const config = scenarioConfigs[options.scenario];
  if (options.agentCount > config.roleLimit) {
    throw new Error(`scenario ${options.scenario} supports at most ${config.roleLimit} agents`);
  }

  const apiKey = resolveApiKey(options);
  const previousRun = options.continueRun
    ? loadPreviousRun(options.continueRun, path.resolve(options.reportDir), options.scenario)
    : null;

  const runId = `${options.scenario}-${options.chapter}-${runToken()}`;
  const runDir = path.join(path.resolve(options.reportDir), runId);
  const piHomeDir = path.join(runDir, "pi-home");
  ensureDir(piHomeDir);

  const anxBin = buildAnxBinary(runDir, options.anxBin);
  const coreBin = buildCoreBinary(runDir, options.coreBin);
  const core = await startManagedCore(runDir, coreBin, options.baseUrl, options.scenario, previousRun);
  const scenarioContent = loadScenarioContent(options.scenario, options.chapter, core.baseUrl);
  const renderedScenario = scenarioContent.combinedMarkdown;
  const sharedTargets = await resolveSharedTargets(core.baseUrl, config);
  const currentChapterState = options.chapter === "chapter-1" && !previousRun
    ? null
    : await collectScenarioState(core.baseUrl, config);
  const currentChapterStateGuide = currentChapterState
    ? chapterStateMarkdown(currentChapterState, previousRun)
    : "";
  const roles = config.roles.slice(0, options.agentCount);

  console.log(`pi dogfood run: ${runId}`);
  console.log(`chapter: ${options.chapter}`);
  console.log(`base url: ${core.baseUrl}`);
  console.log(`agents: ${options.agentCount}`);
  console.log(`max seconds: ${options.maxSeconds}`);
  console.log(`agent start stagger seconds: ${options.agentCount > 1 ? options.agentStartStaggerSeconds : 0}`);
  if (options.agentCount > 1 && options.maxSeconds < 600) {
    console.warn(`warning: --max-seconds ${options.maxSeconds} is below the recommended 600s floor for multi-agent scenario validation`);
  }

  let agentRuns = [];
  let finalChapterState = currentChapterState;
  try {
    const pendingAgents = roles.map((role, agentIndex) => {
      const agentId = `agent-${String(agentIndex + 1).padStart(2, "0")}`;
      const agentUsername = String(
        role.authUsername ?? `${options.agentPrefix}-${role.name}`,
      ).trim();
      const workspaceDir = agentWorkspaceDir(runDir, options.agentCount, agentId);
      const eventsPath = agentEventsPath(runDir, options.agentCount, agentId);
      const resultPath = agentResultPath(workspaceDir);
      const homeDir = agentHomeDir(runDir, agentId);
      return {
        role,
        agentId,
        actorId: String(role.actorId ?? "").trim(),
        agentUsername,
        workspaceDir,
        eventsPath,
        resultPath,
        homeDir,
        profileReady: false,
      };
    });

    const profileReady = previousRun
      ? hydrateContinuationHomes({
          previousRunDir: previousRun.runDir,
          runDir,
          agents: pendingAgents,
        })
      : prepareAgentProfiles({
          anxBin,
          baseUrl: core.baseUrl,
          bootstrapToken: core.bootstrapToken,
          agents: pendingAgents,
        });

    await ensureAgentWakeRegistrations(core.baseUrl, anxBin, pendingAgents);

    const agentPlans = pendingAgents.map((agent) => {
      return {
        ...agent,
        profileReady,
        promise: (async () => {
          const staggerMs =
            options.agentCount > 1
              ? Math.round(options.agentStartStaggerSeconds * 1000 * pendingAgents.indexOf(agent))
              : 0;
          if (staggerMs > 0) {
            // Stagger starts to reduce provider-side 429 bursts on multi-agent dogfood runs.
            await delay(staggerMs);
          }
          return runPiAgent({
            runDir,
            piHomeDir,
            anxBin,
            coreBaseUrl: core.baseUrl,
            provider: options.provider,
            model: options.model,
            apiKey,
            maxSeconds: options.maxSeconds,
            agentId: agent.agentId,
            agentCount: options.agentCount,
            agentUsername: agent.agentUsername,
            homeDir: agent.homeDir,
            profileReady,
            scenarioMarkdown: renderedScenario,
            chapterID: options.chapter,
            chapterMarkdown: scenarioContent.chapterMarkdown,
            chapterState: currentChapterState,
            chapterStateGuide: currentChapterStateGuide,
            scenarioConfig: config,
            role: agent.role,
            targets: roleTargets(config, sharedTargets, agent.role),
          });
        })(),
      };
    });

    const settled = await Promise.allSettled(agentPlans.map((agent) => agent.promise));
    agentRuns = settled.map((result, index) => {
      const pending = agentPlans[index];
      if (result.status === "fulfilled") {
        return { status: "ok", role: pending.role.name, ...result.value };
      }
      return {
        status: "failed",
        role: pending.role.name,
        agentId: pending.agentId,
        agentUsername: pending.agentUsername,
        workspaceDir: pending.workspaceDir,
        eventsPath: pending.eventsPath,
        resultPath: pending.resultPath,
        error: result.reason instanceof Error ? result.reason.message : String(result.reason),
      };
    });

    const failedAgents = agentRuns.filter((agent) => agent.status !== "ok");
    if (failedAgents.length > 0) {
      throw new Error(`pi dogfood failed for ${failedAgents.map((agent) => `${agent.agentId}: ${agent.error}`).join(", ")}`);
    }
    finalChapterState = await collectScenarioState(core.baseUrl, config);
  } finally {
    await core.stop();
  }

  const metadata = {
    run_id: runId,
    scenario: options.scenario,
    chapter: options.chapter,
    continued_from_run_id: previousRun?.metadata?.run_id ?? "",
    continued_from_run_dir: previousRun?.runDir ?? "",
    provider: options.provider,
    model: options.model,
    base_url: core.baseUrl,
    managed_core: core.managed,
    core_workspace_dir: core.workspaceDir,
    core_log_path: core.logPath,
    agent_count: options.agentCount,
    targets: sharedTargets,
    agents: agentRuns,
    anx_bin: anxBin,
    core_bin: coreBin,
    scenario_state_path: finalChapterState ? path.join(runDir, "scenario-state.json") : "",
  };
  writeFile(path.join(runDir, "run-metadata.json"), `${JSON.stringify(metadata, null, 2)}\n`);
  if (finalChapterState) {
    writeFile(path.join(runDir, "scenario-state.json"), `${JSON.stringify(finalChapterState, null, 2)}\n`);
  }

  for (const agent of agentRuns) {
    if (agent.status !== "ok") {
      continue;
    }
    console.log(`workspace (${agent.agentId}): ${agent.workspaceDir}`);
    console.log(`events (${agent.agentId}): ${agent.eventsPath}`);
    console.log(`result (${agent.agentId}): ${agent.resultPath}`);
  }
  console.log(`metadata: ${path.join(runDir, "run-metadata.json")}`);
  if (finalChapterState) {
    console.log(`scenario state: ${path.join(runDir, "scenario-state.json")}`);
  }
}

const isEntrypoint = process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url;

if (isEntrypoint) {
  main().catch((error) => {
    console.error(error.message);
    process.exit(1);
  });
}
