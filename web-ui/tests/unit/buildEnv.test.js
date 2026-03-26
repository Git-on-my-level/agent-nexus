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
OAR_UI_BASE_PATH=/oar
ADAPTER="node"
export FEATURE_FLAG='keep-me'
UNQUOTED=value # inline comment
`),
    ).toEqual({
      ADAPTER: "node",
      FEATURE_FLAG: "keep-me",
      OAR_UI_BASE_PATH: "/oar",
      UNQUOTED: "value",
    });
  });

  it("layers .env.build, .env.build.local, and shell env in order", () => {
    const cwd = createTempDir();

    fs.writeFileSync(
      path.join(cwd, BUILD_ENV_FILENAMES[0]),
      "OAR_UI_BASE_PATH=/from-build\nADAPTER=auto\n",
      "utf8",
    );
    fs.writeFileSync(
      path.join(cwd, BUILD_ENV_FILENAMES[1]),
      "OAR_UI_BASE_PATH=/from-local\n",
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
      OAR_UI_BASE_PATH: "/from-local",
    });
  });

  it("normalizes base path from resolved build config", () => {
    expect(
      resolveUiBuildConfig({
        env: {
          OAR_UI_BASE_PATH: " /oar/ ",
          ADAPTER: "node",
        },
      }),
    ).toEqual({
      basePath: "/oar",
      useNodeAdapter: true,
    });

    expect(normalizeBasePath("/")).toBe("");
    expect(normalizeBasePath(" /oar/// ")).toBe("/oar");
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
  const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "oar-ui-build-env-"));
  tempDirs.push(tempDir);
  return tempDir;
}
