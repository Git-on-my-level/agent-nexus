import { execFileSync } from "node:child_process";
import { mkdtempSync, writeFileSync, chmodSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";

import { describe, expect, it } from "vitest";

import {
  buildMachinePrompt,
  buildPmPrompt,
} from "../../src/lib/setup/setupPrompt.js";

/**
 * The prompts are run by an agent, unread, on someone else's computer. Asserting
 * on their wording cannot catch a snippet that does not parse, or one whose
 * arguments arrive mangled — so these run the snippets through a real shell
 * against a stub `anx` that records its argv.
 */

const BASE = {
  workspaceLabel: "Acme Ops",
  cliBaseUrl: "https://anx.example.test/o/acme/w/ops",
  token: "htok_se'cret\"$(id)`x`",
  expiresAt: "2026-10-09T09:41:00Z",
};

/** Every indented line of a prompt, as the shell would see it. */
function snippets(prompt) {
  const out = [];
  let current = [];
  for (const line of prompt.split("\n")) {
    if (line.startsWith("     ")) {
      current.push(line.slice(5));
      continue;
    }
    if (current.length) {
      out.push(current.join("\n"));
      current = [];
    }
  }
  if (current.length) out.push(current.join("\n"));
  return out;
}

function stubAnx() {
  const dir = mkdtempSync(path.join(tmpdir(), "anx-stub-"));
  const log = path.join(dir, "argv.log");
  const bin = path.join(dir, "anx");
  writeFileSync(
    bin,
    `#!/bin/sh
{
  printf 'CALL'
  for a in "$@"; do printf '\\037%s' "$a"; done
  printf '\\n'
} >> ${JSON.stringify(log)}
cat >/dev/null 2>&1 || true
exit 0
`,
  );
  chmodSync(bin, 0o755);
  return { dir, log, bin };
}

function runSnippet(snippet, { dir, log }) {
  execFileSync("sh", ["-c", snippet], {
    env: { PATH: `${dir}:/usr/bin:/bin`, HOME: dir },
    encoding: "utf8",
  });
  return readFileSync(log, "utf8")
    .split("\n")
    .filter(Boolean)
    .map((line) => line.split("\u001f").slice(1));
}

describe("every generated snippet parses", () => {
  it.each([
    ["machine", buildMachinePrompt(BASE)],
    ["pm", buildPmPrompt({ ...BASE, runnerKey: "claude" })],
  ])("%s prompt", (_kind, prompt) => {
    const parts = snippets(prompt);
    /*
     * An exact count, not a floor: a command that drifts out of the indented
     * block stops being covered by everything below, and a floor would keep
     * passing while it did.
     */
    expect(parts).toHaveLength(_kind === "pm" ? 7 : 6);
    for (const part of parts) {
      // `sh -n` parses without running: a broken line continuation or an
      // unbalanced quote fails here.
      expect(() =>
        execFileSync("sh", ["-n", "-c", part], { encoding: "utf8" }),
      ).not.toThrow();
    }
  });
});

describe("the enrollment pipeline delivers what it says", () => {
  it("passes the token on stdin and a lowercase host name in argv", () => {
    const stub = stubAnx();
    const enroll = snippets(buildMachinePrompt(BASE)).find((part) =>
      part.includes("host enroll"),
    );
    expect(enroll).toBeTruthy();
    const calls = runSnippet(enroll, stub);
    expect(calls).toHaveLength(1);
    const argv = calls[0];
    expect(argv).toContain("host");
    expect(argv).toContain("enroll");
    expect(argv).toContain("--token-stdin");
    // The secret is not in argv, where `ps` would show it.
    expect(argv.join(" ")).not.toContain("htok_");
    // One `--name` word, lowercased, not split by the command substitution.
    const name = argv[argv.indexOf("--name") + 1];
    expect(name).toBeTruthy();
    expect(name).toBe(name.toLowerCase());
    expect(name.split(/\s/)).toHaveLength(1);
  });

  it("keeps a hostile token inside one argument", () => {
    const stub = stubAnx();
    const enroll = snippets(buildMachinePrompt(BASE)).find((part) =>
      part.includes("host enroll"),
    );
    const out = execFileSync("sh", ["-c", `${enroll} && echo DONE`], {
      env: { PATH: `${stub.dir}:/usr/bin:/bin`, HOME: stub.dir },
      encoding: "utf8",
    });
    // No command substitution in the token ran, and nothing broke the pipe.
    expect(out).toContain("DONE");
    expect(out).not.toMatch(/uid=/);
  });
});

describe("the PM install line", () => {
  it.each(["claude", "hermes"])(
    "delivers the %s runner as a single argument",
    (runnerKey) => {
      const stub = stubAnx();
      const install = snippets(buildPmPrompt({ ...BASE, runnerKey })).find(
        (part) => part.includes("pm install"),
      );
      expect(install).toBeTruthy();
      const argv = runSnippet(install, stub)[0];
      const runner = argv[argv.indexOf("--runner") + 1];
      expect(runner).toContain("{prompt_file}");
      // `"$1"` in the Claude runner must survive unexpanded.
      if (runnerKey === "claude") {
        expect(runner).toBe(`sh -c 'exec claude -p < "$1"' sh {prompt_file}`);
      }
      expect(argv).toContain("--wait");
    },
  );
});

describe("verification does not need an agent identity", () => {
  /*
   * `auth whoami` and `host list` fail with `identity_unresolved` on a
   * perfectly enrolled machine whose shell carries no harness marker, so a
   * prompt built on them reports a working enrollment as a failure. `doctor`
   * reports enrollment as its own check and needs no identity.
   */
  it.each([
    ["machine", buildMachinePrompt(BASE)],
    ["pm", buildPmPrompt(BASE)],
  ])("%s prompt verifies with doctor or pm status", (_kind, prompt) => {
    expect(prompt).toContain("--json doctor");
    expect(prompt).toContain("host_enrollment");
    /*
     * Over the whole body, not just the snippet set: scoping this to indented
     * lines would stop covering a command that drifts out of them, while still
     * passing.
     */
    expect(prompt).not.toContain("auth whoami");
    expect(prompt).not.toContain("host list");
  });

  it("tells the PM prompt to skip enrollment when doctor says it is enrolled", () => {
    const prompt = buildPmPrompt(BASE);
    expect(prompt.indexOf("--json doctor")).toBeLessThan(
      prompt.indexOf("host enroll"),
    );
    expect(prompt).toContain("leave");
    expect(prompt).toContain("the token unspent");
  });
});

describe("the doctor check is described as it behaves", () => {
  /*
   * `anx doctor` exits 0 whether or not its checks pass, and reports checks
   * that are red on a correctly set up machine. An agent reading the exit
   * status would call a failed enrollment proven; one reading every red check
   * would report a working machine as broken.
   */
  it.each([
    ["machine", buildMachinePrompt(BASE)],
    ["pm", buildPmPrompt(BASE)],
  ])("%s prompt", (_kind, prompt) => {
    expect(prompt).toContain("exits 0 whether or not");
    expect(prompt).toContain("identity_resolution");
    expect(prompt).toContain("agentctl_presence");
  });

  it("really does exit 0 with the enrollment check failing", () => {
    // Guards the sentence above against the CLI changing under it.
    const stub = stubAnx();
    expect(() =>
      execFileSync("sh", ["-c", "anx --json doctor"], {
        env: { PATH: `${stub.dir}:/usr/bin:/bin`, HOME: stub.dir },
      }),
    ).not.toThrow();
  });
});

describe("the install step leaves anx reachable", () => {
  it.each([
    ["machine", buildMachinePrompt(BASE)],
    ["pm", buildPmPrompt(BASE)],
  ])("%s prompt says where the installer puts it", (_kind, prompt) => {
    // The installer writes ~/.local/bin, which is not on PATH by default on
    // macOS; without this the one-paste flow dead-ends on a fresh machine.
    expect(prompt).toContain("~/.local/bin");
    /*
     * A prefix, not an export: harnesses that run each command in its own
     * shell — Claude Code, the default PM runner — drop exported env between
     * calls, so an export would leave the next command failing the same way.
     */
    expect(prompt).toContain('PATH="$HOME/.local/bin:$PATH" anx');
    expect(prompt).not.toContain("export PATH=");
  });
});
