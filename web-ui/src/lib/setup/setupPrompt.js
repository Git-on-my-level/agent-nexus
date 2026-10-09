/**
 * The text the reader hands to an agent to set this workspace up on a machine.
 *
 * One prompt per outcome — a machine in the workspace, a PM running on it —
 * built from values this deployment already knows. Pure functions only: the
 * component owns the token's lifetime, this owns what the words are.
 *
 * Two rules the wording exists to keep:
 *   - the token is single-use and short-lived, and the prompt says so, so an
 *     agent has no reason to keep it anywhere;
 *   - everything only a person may do (passkeys, invitations, administration)
 *     is named as off-limits, because an agent that cannot finish is otherwise
 *     an agent looking for another way in.
 */

import { pmRunnerFor } from "./pmRunners.js";

/**
 * How a reader installs `anx` when the deployment has not said otherwise.
 *
 * The public OSS installer. A deployment that ships its own build overrides
 * this with `ANX_UI_CLI_INSTALL_COMMAND`; nothing here is hosted-specific.
 */
export const DEFAULT_CLI_INSTALL_COMMAND =
  "curl -sSfL https://raw.githubusercontent.com/Git-on-my-level/agent-nexus/main/scripts/install-anx.sh | sh";

/** Lifetime of the enrollment token a setup prompt carries. */
export const SETUP_TOKEN_LIFETIME_MS = 30 * 60_000;

/**
 * The same lifetime as core measures it.
 *
 * Sent instead of `expires_at` so the window is the server's 30 minutes and
 * not the browser's: a clock a few minutes slow makes core reject the absolute
 * form outright, and a clock a day fast would buy a 23-hour credential while
 * the countdown on screen claimed half an hour.
 */
export const SETUP_TOKEN_LIFETIME_SECONDS = SETUP_TOKEN_LIFETIME_MS / 1000;

/** Label recorded on tokens issued for a copyable prompt. */
export const SETUP_TOKEN_LABEL = "Setup prompt";

function text(value) {
  return String(value ?? "").trim();
}

/**
 * @param {string} [value] deployment override
 */
export function resolveCliInstallCommand(value) {
  return text(value) || DEFAULT_CLI_INSTALL_COMMAND;
}

/**
 * Quote a value as one POSIX shell word.
 *
 * Single quotes, always: a runner argv contains `"$1"`, and inside double
 * quotes the shell would expand it before the CLI ever saw the placeholder.
 *
 * @param {string} value
 */
export function shellQuote(value) {
  return `'${String(value ?? "").replaceAll("'", `'\\''`)}'`;
}

/**
 * Whether a base URL points at the machine running the browser.
 *
 * A copied command carrying one of these is merely wrong; a copied *prompt*
 * carrying one is worse, because the agent runs it without reading it and
 * reports a connection failure that looks like the workspace being down. The
 * caller withholds the prompt instead of shipping a broken one.
 *
 * @param {string} baseUrl
 */
export function isLoopbackBaseUrl(baseUrl) {
  const raw = text(baseUrl);
  if (!raw) return false;
  let hostname = "";
  try {
    hostname = new URL(raw).hostname.toLowerCase();
  } catch {
    return false;
  }
  if (hostname.startsWith("[") && hostname.endsWith("]")) {
    hostname = hostname.slice(1, -1);
  }
  // `localhost.` is the same name with an explicit root label.
  if (hostname.endsWith(".") && hostname.length > 1) {
    hostname = hostname.slice(0, -1);
  }
  if (hostname === "localhost" || hostname.endsWith(".localhost")) return true;
  // The whole of 127.0.0.0/8 is this machine, not just .0.1. `URL` has already
  // normalized shorthand forms (`127.1`, `2130706433`, `0x7f.1`) to dotted quads.
  if (/^127\.\d{1,3}\.\d{1,3}\.\d{1,3}$/.test(hostname)) return true;
  if (hostname === "0.0.0.0" || hostname === "0") return true;
  // IPv6 loopback and unspecified, including the IPv4-mapped spellings `URL`
  // compresses to `::ffff:7f00:1`.
  if (hostname === "::1" || hostname === "::") return true;
  if (/^::ffff:7f[0-9a-f]{2}:[0-9a-f]{1,4}$/.test(hostname)) return true;
  if (/^::ffff:127\.\d{1,3}\.\d{1,3}\.\d{1,3}$/.test(hostname)) return true;
  return false;
}

/**
 * Why a setup prompt cannot be offered, or "" when it can.
 *
 * @param {{ cliBaseUrl?: string }} options
 */
export function setupPromptBlockedReason({ cliBaseUrl = "" } = {}) {
  if (!text(cliBaseUrl)) {
    return "This deployment has not told the web app its API address, so a setup prompt would have nowhere to point. Set coreBaseUrl on the workspace entry.";
  }
  if (isLoopbackBaseUrl(cliBaseUrl)) {
    return `This workspace's API address is ${text(cliBaseUrl)}, which means "the machine you are reading this on". A prompt carrying it would send your agent to its own computer. Set coreBaseUrl or publicOrigin to an address other machines can reach.`;
  }
  return "";
}

/** `anx --base-url <url>` prefix, or bare `anx` when there is no base URL. */
export function anxCommand(baseUrl, rest) {
  const base = text(baseUrl);
  return `anx ${base ? `--base-url ${base} ` : ""}${rest}`;
}

/** The interactive enrollment command, for the reader who runs it themselves. */
export function hostEnrollCommand({ cliBaseUrl = "" } = {}) {
  return anxCommand(cliBaseUrl, "host enroll");
}

/**
 * Absolute, unambiguous expiry wording. The agent reads this to decide whether
 * to bother, so a relative "in 30 minutes" would be wrong by the time it lands.
 *
 * @param {string} expiresAt ISO 8601
 */
export function formatPromptExpiry(expiresAt) {
  const at = Date.parse(text(expiresAt));
  if (!Number.isFinite(at)) return "";
  return `${new Date(at).toISOString().replace("T", " ").slice(0, 16)} UTC`;
}

/**
 * Countdown for the reader, as `m:ss`. Empty once it has run out, so the
 * caller says "expired" in words rather than counting below zero.
 *
 * @param {number} remainingMs
 */
export function formatCountdown(remainingMs) {
  if (!Number.isFinite(remainingMs) || remainingMs <= 0) return "";
  const total = Math.floor(remainingMs / 1000);
  return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, "0")}`;
}

function installStep(index, installCommand) {
  return [
    `${index}. Make sure the anx CLI is installed (macOS or Linux, Python 3.8+):`,
    "",
    `     anx --version || ${installCommand}`,
    "",
    "   The installer puts anx in ~/.local/bin, which is not on PATH by default on",
    "   macOS. If `anx --version` still fails after installing, add it for this",
    "   session and try again:",
    "",
    '     export PATH="$HOME/.local/bin:$PATH"',
    "",
    "   Stop and tell me if it still fails.",
  ];
}

function skillStep(index) {
  return [
    `${index}. Optional, recommended: install the ANX skill into the harnesses on this`,
    "   machine, so you know how to use anx later. It only writes copies it owns:",
    "",
    "     anx skills sync",
  ];
}

const HUMAN_ONLY = [
  "Do not create passkeys, accept invitations or grant administration — those are",
  "mine to do in the web app. If the token is expired or already used, stop and ask",
  "me for a new one instead of looking for another way in.",
];

/**
 * Connect a machine to this workspace.
 *
 * @param {{ workspaceLabel?: string, cliBaseUrl?: string, installCommand?: string, token?: string, expiresAt?: string }} options
 */
export function buildMachinePrompt({
  workspaceLabel = "",
  cliBaseUrl = "",
  installCommand = "",
  token = "",
  expiresAt = "",
} = {}) {
  const base = text(cliBaseUrl);
  const install = resolveCliInstallCommand(installCommand);
  const expiry = formatPromptExpiry(expiresAt);
  return [
    `Set up this computer as an Agent Nexus machine for the workspace "${text(workspaceLabel)}", then report back. Run everything non-interactively; nothing below needs a human.`,
    "",
    ...installStep(1, install),
    "",
    `2. Enroll this computer. The token below is single-use${expiry ? ` and expires at ${expiry}` : ""}.`,
    "   Do not write it to a file, a dotfile, your notes or your memory, and do not",
    "   run this twice:",
    "",
    `     printf %s ${shellQuote(token)} \\`,
    `       | ${anxCommand(base, "host enroll --token-stdin")} \\`,
    `           --name "$(hostname -s | tr A-Z a-z)"`,
    "",
    "3. Prove it worked with a real call, not by re-reading the output above:",
    "",
    `     ${anxCommand(base, "--json doctor")}`,
    "",
    "   Read the `host_enrollment` check in that output: ok means this computer is",
    "   in the workspace. Do not use `auth whoami` or `host list` here — they need",
    "   an agent identity this shell may not have, and would report a working",
    "   enrollment as a failure.",
    "",
    ...skillStep(4),
    "",
    "5. Report back: the host slug that was created, the agent harnesses anx",
    "   discovered on this machine, and anything that failed.",
    "",
    ...HUMAN_ONLY,
  ].join("\n");
}

/**
 * Run a PM on this computer — including enrolling the machine if it is not
 * enrolled yet, so one paste is the whole job.
 *
 * The enrollment step is conditional rather than omitted: the web app knows
 * whether *a* machine is enrolled in this workspace, never whether *this* one
 * is, and a prompt that assumes wrongly either burns a token re-enrolling an
 * enrolled machine or stops to ask for one. The agent decides on the spot from
 * `anx doctor`, and the token is spent only when it is needed.
 *
 * `doctor` rather than a shell test on `auth whoami`: whoami fails with
 * `identity_unresolved` on a perfectly enrolled machine whose shell carries no
 * harness marker, so a gate built on it would re-enrol a machine that is
 * already in the workspace. `doctor` needs no agent identity and reports
 * enrollment as its own check.
 *
 * @param {{ workspaceLabel?: string, cliBaseUrl?: string, installCommand?: string, token?: string, expiresAt?: string, runnerKey?: string }} options
 */
export function buildPmPrompt({
  workspaceLabel = "",
  cliBaseUrl = "",
  installCommand = "",
  token = "",
  expiresAt = "",
  runnerKey = "",
} = {}) {
  const base = text(cliBaseUrl);
  const install = resolveCliInstallCommand(installCommand);
  const expiry = formatPromptExpiry(expiresAt);
  const runner = pmRunnerFor(runnerKey);
  return [
    `Set up the Agent Nexus PM service on this computer for the workspace "${text(workspaceLabel)}", then report back. Run everything non-interactively; nothing below needs a human.`,
    "",
    ...installStep(1, install),
    "",
    "2. Join this computer to the workspace, but only if it is not in it already.",
    "   Check first:",
    "",
    `     ${anxCommand(base, "--json doctor")}`,
    "",
    "   Read the `host_enrollment` check. If it is ok, this computer is already",
    "   enrolled: skip the rest of this step and leave the token unspent. Only if",
    `   it is not ok, run this. The token is single-use${expiry ? ` and expires at ${expiry}` : ""}; do not`,
    "   write it to a file, a dotfile, your notes or your memory:",
    "",
    `     printf %s ${shellQuote(token)} \\`,
    `       | ${anxCommand(base, "host enroll --token-stdin")} \\`,
    `           --name "$(hostname -s | tr A-Z a-z)"`,
    "",
    "3. Install the PM service. `anx pm install` with no --runner opens an",
    "   interactive wizard you cannot answer, so pass the runner explicitly.",
    `   This workspace is set to run the PM with ${runner.label}:`,
    "",
    `     ${anxCommand(base, `pm install --runner ${shellQuote(runner.argv)} --wait`)}`,
    "",
    "   --wait blocks until the PM's first connection is accepted (90s by default).",
    "",
    "4. Verify:",
    "",
    `     ${anxCommand(base, "--json pm status")}`,
    "",
    "   `pm status` reads the local service and needs no agent identity.",
    "",
    ...skillStep(5),
    "",
    "6. Report back: the host slug, the PM service id, the log directory, and",
    "   whether the first connection was accepted. If it was not, send me the last",
    "   20 lines of stderr.log from that log directory.",
    "",
    "The PM runs as you, on this computer. It holds no workspace secret of its own:",
    "it authenticates through this machine's host identity.",
    ...HUMAN_ONLY,
  ].join("\n");
}
