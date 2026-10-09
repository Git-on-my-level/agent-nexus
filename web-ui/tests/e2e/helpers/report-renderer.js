import { execFile } from "node:child_process";
import { access } from "node:fs/promises";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);

export async function runReportRenderer(command, args, options, outputPath) {
  const sandbox = process.env.ANX_TEST_REPORT_CHROMIUM_SANDBOX;
  const rendererArgs = sandbox
    ? [...args, "--chromium-sandbox", sandbox]
    : args;
  let result;
  try {
    result = await execFileAsync(command, rendererArgs, options);
  } catch (error) {
    console.error(error.stderr || error.message);
    throw new Error(
      `Report renderer failed: ${error.message}\nstdout:\n${error.stdout ?? ""}\nstderr:\n${error.stderr ?? ""}`,
      { cause: error },
    );
  }
  if (result.stderr) console.error(result.stderr);
  try {
    await access(outputPath);
  } catch (error) {
    throw new Error(
      `Report renderer exited without writing ${outputPath}\nstdout:\n${result.stdout}\nstderr:\n${result.stderr}`,
      { cause: error },
    );
  }
  return result;
}
