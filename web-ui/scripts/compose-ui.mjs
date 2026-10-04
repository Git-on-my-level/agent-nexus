import {
  cpSync,
  existsSync,
  mkdirSync,
  readFileSync,
  readdirSync,
  rmSync,
  symlinkSync,
} from "node:fs";
import path from "node:path";

function files(root, prefix = "") {
  return readdirSync(path.join(root, prefix), { withFileTypes: true }).flatMap(
    (entry) => {
      const rel = path.join(prefix, entry.name);
      if (entry.isSymbolicLink())
        throw new Error(`Extension symlinks are unsupported: ${rel}`);
      return entry.isDirectory() ? files(root, rel) : [rel];
    },
  );
}

/** Compose an application outside both source trees, with explicit replacement ownership. */
export function composeUi({ sourceRoot, extensionRoot, outputRoot }) {
  sourceRoot = path.resolve(sourceRoot);
  extensionRoot = path.resolve(extensionRoot);
  outputRoot = path.resolve(outputRoot);
  for (const root of [sourceRoot, extensionRoot]) {
    if (
      outputRoot === root ||
      root.startsWith(`${outputRoot}${path.sep}`) ||
      outputRoot.startsWith(`${root}${path.sep}`)
    ) {
      throw new Error(
        "Composition output must be outside source and extension trees",
      );
    }
  }
  const manifest = JSON.parse(
    readFileSync(path.join(extensionRoot, "extension.json"), "utf8"),
  );
  const replacements = new Set(manifest.replace ?? []);
  const additions = manifest.add ?? [];
  const overlays = [];
  for (const tree of ["src", "tests", "static"]) {
    const root = path.join(extensionRoot, tree);
    if (!existsSync(root)) continue;
    for (const rel of files(root)) {
      const target = `${tree}/${rel.split(path.sep).join("/")}`;
      const collides = existsSync(path.join(sourceRoot, target));
      if (
        collides
          ? !replacements.has(target)
          : !additions.some(
              (prefix) => target === prefix || target.startsWith(`${prefix}/`),
            )
      ) {
        throw new Error(
          `Undeclared extension ${collides ? "replacement" : "addition"}: ${target}`,
        );
      }
      overlays.push(target);
    }
  }
  for (const target of replacements) {
    if (!overlays.includes(target))
      throw new Error(`Missing extension replacement: ${target}`);
  }
  rmSync(outputRoot, { recursive: true, force: true });
  mkdirSync(outputRoot, { recursive: true });
  for (const entry of readdirSync(sourceRoot, { withFileTypes: true })) {
    if (
      [
        "node_modules",
        ".svelte-kit",
        "build",
        "coverage",
        "test-results",
        "playwright-report",
      ].includes(entry.name) ||
      entry.name.startsWith(".env") ||
      entry.name.startsWith(".qa")
    )
      continue;
    cpSync(
      path.join(sourceRoot, entry.name),
      path.join(outputRoot, entry.name),
      { recursive: true },
    );
  }
  for (const target of overlays) {
    mkdirSync(path.dirname(path.join(outputRoot, target)), { recursive: true });
    cpSync(path.join(extensionRoot, target), path.join(outputRoot, target));
  }
  symlinkSync(
    path.join(sourceRoot, "node_modules"),
    path.join(outputRoot, "node_modules"),
    "dir",
  );
  return outputRoot;
}
