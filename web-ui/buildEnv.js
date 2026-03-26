import fs from "node:fs";
import path from "node:path";

import { normalizeBasePath as sharedNormalizeBasePath } from "./src/lib/pathUtils.js";

export const BUILD_ENV_FILENAMES = [".env.build", ".env.build.local"];
export const normalizeBasePath = sharedNormalizeBasePath;

export function parseBuildEnvFile(contents = "") {
  const parsed = {};

  for (const rawLine of String(contents).split(/\r?\n/)) {
    const entry = parseBuildEnvLine(rawLine);
    if (entry) {
      parsed[entry.key] = entry.value;
    }
  }

  return parsed;
}

export function loadBuildEnvFiles({
  cwd = process.cwd(),
  filenames = BUILD_ENV_FILENAMES,
  existsSync = fs.existsSync,
  readFileSync = fs.readFileSync,
} = {}) {
  const parsed = {};

  for (const filename of filenames) {
    const filePath = path.join(cwd, filename);
    if (!existsSync(filePath)) {
      continue;
    }

    Object.assign(parsed, parseBuildEnvFile(readFileSync(filePath, "utf8")));
  }

  return parsed;
}

export function resolveBuildEnv(options = {}) {
  const { env = process.env } = options;

  return {
    ...loadBuildEnvFiles(options),
    ...env,
  };
}

export function resolveUiBuildConfig(options = {}) {
  const env = resolveBuildEnv(options);
  const adapter = String(env.ADAPTER ?? "node").trim() || "node";

  return {
    basePath: normalizeBasePath(env.OAR_UI_BASE_PATH),
    useNodeAdapter: adapter === "node",
  };
}

function parseBuildEnvLine(rawLine = "") {
  const line = String(rawLine);
  const trimmed = line.trim();
  if (!trimmed || trimmed.startsWith("#")) {
    return null;
  }

  const match = line.match(
    /^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)\s*$/,
  );
  if (!match) {
    return null;
  }

  const [, key, rawValue] = match;
  return {
    key,
    value: parseBuildEnvValue(rawValue),
  };
}

function parseBuildEnvValue(rawValue = "") {
  const trimmed = String(rawValue).trim();
  if (!trimmed) {
    return "";
  }

  const quote = trimmed[0];
  if (quote === '"' || quote === "'") {
    const closingIndex = findClosingQuote(trimmed, quote);
    if (closingIndex === -1) {
      return trimmed.slice(1);
    }

    const quotedValue = trimmed.slice(0, closingIndex + 1);
    return quote === '"' ? JSON.parse(quotedValue) : quotedValue.slice(1, -1);
  }

  return trimmed.replace(/\s+#.*$/, "").trim();
}

function findClosingQuote(value, quote) {
  let escaped = false;

  for (let index = 1; index < value.length; index += 1) {
    const char = value[index];

    if (quote === '"' && char === "\\" && !escaped) {
      escaped = true;
      continue;
    }

    if (char === quote && !escaped) {
      return index;
    }

    escaped = false;
  }

  return -1;
}
