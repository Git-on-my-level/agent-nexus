import { readFile } from "node:fs/promises";
import { error } from "@sveltejs/kit";

async function readJSON(path) {
  if (!path) error(404, "Preview is available only from the local renderer.");
  try {
    return JSON.parse(await readFile(path, "utf8"));
  } catch {
    error(404, "Preview input is unavailable.");
  }
}

export async function load() {
  const reportPath = process.env.ANX_REPORT_PREVIEW_REPORT;
  const observationsPath = process.env.ANX_REPORT_PREVIEW_OBSERVATIONS;
  if (!reportPath || !observationsPath)
    error(404, "Preview is available only from the local renderer.");
  return {
    report: await readJSON(reportPath),
    observations: await readJSON(observationsPath),
  };
}
