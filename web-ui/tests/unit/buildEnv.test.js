import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import {
  BUILD_ENV_FILENAMES,
  normalizeBasePath,
  parseBuildEnvFile,
  resolveBuildEnv,
  resolveUiBuildConfig,
} from "../../buildEnv.js";

const tempDirs = [];

afterEach(() => {
  for (const tempDir of tempDirs.splice(0)) {
    fs.rmSync(tempDir, { force: true, recursive: true });
  }
});

describe("buildEnv", () => {
  it("parses .env.build style assignments", () => {
    expect(
      parseBuildEnvFile(`
# comment
ANX_UI_BASE_PATH=/anx
ADAPTER="node"
export FEATURE_FLAG='keep-me'
UNQUOTED=value # inline comment
`),
    ).toEqual({
      ADAPTER: "node",
      FEATURE_FLAG: "keep-me",
      ANX_UI_BASE_PATH: "/anx",
      UNQUOTED: "value",
    });
  });

  it("layers .env.build, .env.build.local, and shell env in order", () => {
    const cwd = createTempDir();

    fs.writeFileSync(
      path.join(cwd, BUILD_ENV_FILENAMES[0]),
      "ANX_UI_BASE_PATH=/from-build\nADAPTER=auto\n",
      "utf8",
    );
    fs.writeFileSync(
      path.join(cwd, BUILD_ENV_FILENAMES[1]),
      "ANX_UI_BASE_PATH=/from-local\n",
      "utf8",
    );

    expect(
      resolveBuildEnv({
        cwd,
        env: {
          ADAPTER: "node",
        },
      }),
    ).toMatchObject({
      ADAPTER: "node",
      ANX_UI_BASE_PATH: "/from-local",
    });
  });

  it("normalizes base path from resolved build config", () => {
    expect(
      resolveUiBuildConfig({
        env: {
          ANX_UI_BASE_PATH: " /anx/ ",
          ADAPTER: "node",
        },
      }),
    ).toEqual({
      basePath: "/anx",
      useNodeAdapter: true,
    });

    expect(normalizeBasePath("/")).toBe("");
    expect(normalizeBasePath(" /anx/// ")).toBe("/anx");
  });

  it("defaults to the node adapter when ADAPTER is unset", () => {
    const cwd = createTempDir();

    expect(
      resolveUiBuildConfig({
        cwd,
        env: {},
      }),
    ).toEqual({
      basePath: "",
      useNodeAdapter: true,
    });
  });
});

function createTempDir() {
  const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "anx-ui-build-env-"));
  tempDirs.push(tempDir);
  return tempDir;
}
