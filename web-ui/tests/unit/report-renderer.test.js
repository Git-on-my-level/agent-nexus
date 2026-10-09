import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { main, run } from "../../scripts/preview-visual-report.mjs";
import { runReportRenderer } from "../e2e/helpers/report-renderer.js";

let directory;
let args;
beforeEach(async () => {
  directory = await mkdtemp(join(tmpdir(), "anx-renderer-test-"));
  await writeFile(join(directory, "report.json"), "{}");
  await writeFile(join(directory, "observations.json"), "[]");
  args = [
    "--report",
    join(directory, "report.json"),
    "--observations",
    join(directory, "observations.json"),
    "--output",
    join(directory, "preview.png"),
  ];
  vi.spyOn(process.stderr, "write").mockReturnValue(true);
  vi.spyOn(console, "error").mockImplementation(() => {});
});
afterEach(async () => {
  vi.restoreAllMocks();
  vi.unstubAllEnvs();
  await rm(directory, { recursive: true, force: true });
});

describe("report renderer sandbox contract", () => {
  it.each([
    [[], true],
    [["--chromium-sandbox", "true"], true],
    [["--chromium-sandbox", "false"], false],
  ])("launches with explicit policy %j", async (flags, enabled) => {
    const launchBrowser = vi.fn(async () => {
      throw new Error("Chromium sandboxing failed!");
    });
    expect(await main([...args, ...flags], { launchBrowser })).toEqual({
      rendered: false,
      reason: "sandbox_unavailable",
    });
    expect(launchBrowser).toHaveBeenCalledWith({
      headless: true,
      channel: "chromium",
      chromiumSandbox: enabled,
    });
    expect(process.stderr.write).toHaveBeenCalledWith(
      "Chromium sandboxing failed!\n",
    );
  });
  it("rejects ambiguous sandbox settings before launching", async () => {
    const launchBrowser = vi.fn();
    await expect(
      main([...args, "--chromium-sandbox", "0"], { launchBrowser }),
    ).rejects.toThrow("must be true or false");
    expect(launchBrowser).not.toHaveBeenCalled();
  });
  it("preserves machine-readable fallback stdout and success status", async () => {
    const stdout = vi.spyOn(process.stdout, "write").mockReturnValue(true);
    expect(
      await run(args, {
        launchBrowser: async () => {
          throw new Error("Chromium sandboxing failed!");
        },
      }),
    ).toBe(0);
    expect(JSON.parse(stdout.mock.calls[0][0])).toEqual({
      rendered: false,
      reason: "sandbox_unavailable",
    });
  });
});

describe("child renderer diagnostics", () => {
  it("reports stderr and text fallback when a zero-status child produces no PNG", async () => {
    vi.stubEnv("ANX_TEST_REPORT_CHROMIUM_SANDBOX", "");
    await expect(
      runReportRenderer(
        process.execPath,
        [
          "-e",
          'console.error("sandbox detail"); console.log("sandbox_unavailable")',
        ],
        {},
        join(directory, "missing.png"),
      ),
    ).rejects.toThrow(/sandbox_unavailable\n+stderr:\nsandbox detail/);
    expect(console.error).toHaveBeenCalledWith("sandbox detail\n");
  });
  it("reports stderr when the child fails", async () => {
    vi.stubEnv("ANX_TEST_REPORT_CHROMIUM_SANDBOX", "");
    await expect(
      runReportRenderer(
        process.execPath,
        ["-e", 'console.error("browser missing"); process.exitCode=1'],
        {},
        join(directory, "missing.png"),
      ),
    ).rejects.toThrow("browser missing");
    expect(console.error).toHaveBeenCalledWith("browser missing\n");
  });
  it("passes the CI sandbox opt-out only to normal rendering calls", async () => {
    vi.stubEnv("ANX_TEST_REPORT_CHROMIUM_SANDBOX", "false");
    const script =
      'require("node:fs").writeFileSync(process.argv[1], process.argv.slice(2).join(" "));';
    const output = join(directory, "arguments.txt");
    await runReportRenderer(
      process.execPath,
      ["-e", script, "--", output],
      {},
      output,
    );
    const { readFile } = await import("node:fs/promises");
    expect(await readFile(output, "utf8")).toBe("--chromium-sandbox false");
  });
});
