#!/usr/bin/env node
import { readFileSync } from "node:fs";
import { parseVisualReport } from "../src/lib/visualReports.js";

const input = process.argv[2];
if (!input || process.argv.length !== 3) {
  console.error(
    "Usage: node web-ui/scripts/validate-visual-report.mjs <report.json|->",
  );
  process.exitCode = 2;
} else {
  try {
    const content = readFileSync(input === "-" ? 0 : input, "utf8");
    const result = parseVisualReport(content);
    if (!result.report) {
      console.error(
        JSON.stringify({
          valid: false,
          errors: result.errors.length
            ? result.errors
            : ["Not an anx.visual-report document"],
        }),
      );
      process.exitCode = 1;
    } else {
      console.log(
        JSON.stringify({
          valid: true,
          schema_version: result.report.schema_version,
          projects: result.report.projects.length,
          panels: result.report.panels.length,
        }),
      );
    }
  } catch {
    console.error(
      JSON.stringify({ valid: false, errors: ["Could not read report input"] }),
    );
    process.exitCode = 1;
  }
}
