import { existsSync, readdirSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

export const commercialMarkers = [
  "PLAN_CARDS",
  "PLAN_USAGE_LIMIT_FALLBACK",
  "tierEnvelopeForBillingSummary",
  "Upgrade to Pro",
  "Upgrade to Scale",
  "Enterprise%20plan%20inquiry",
  "Good for weekend projects or a small business.",
  "/hosted/billing",
  "/hosted/organizations",
  "/hosted/signup",
  "/hosted/",
  "Talk to sales",
];
const forbiddenImport =
  /(?:["'])(?:[^"'\n]*\/)?(?:hosted\/(?:planCatalog|billingActivation|oauthFlow|adminAuth|session)|server\/outOfWorkspace\/(?:hosted|cpClient)|server\/hostedControlPlaneAllowlist)(?:\.js)?["']/;
const commercialPrice = /["']?price["']?\s*:\s*["']\$\d+(?:\.\d+)?["']/;

function walk(root) {
  if (!existsSync(root)) return [];
  return readdirSync(root, { withFileTypes: true }).flatMap((entry) => {
    const file = path.join(root, entry.name);
    return entry.isDirectory() ? walk(file) : entry.isFile() ? [file] : [];
  });
}

export function checkCommercialBoundary({ sourceRoot, bundleRoot }) {
  const violations = [];
  if (!existsSync(sourceRoot))
    violations.push(`Missing source tree: ${sourceRoot}`);
  for (const dir of [
    "lib/hosted",
    "routes/hosted",
    "routes/billing",
    "routes/checkout",
    "routes/plans",
  ]) {
    if (existsSync(path.join(sourceRoot, dir)))
      violations.push(`Forbidden source directory: ${dir}`);
  }
  for (const file of [
    ...walk(sourceRoot),
    ...(bundleRoot ? walk(bundleRoot) : []),
  ]) {
    if (!/\.(?:[cm]?[jt]sx?|svelte|html|json|map)$/.test(file)) continue;
    const contents = readFileSync(file, "utf8");
    for (const marker of commercialMarkers) {
      if (contents.includes(marker)) violations.push(`${file}: ${marker}`);
    }
    if (forbiddenImport.test(contents))
      violations.push(`${file}: commercial import`);
    if (commercialPrice.test(contents)) violations.push(`${file}: plan price`);
  }
  if (bundleRoot && !existsSync(bundleRoot))
    violations.push(`Missing build output: ${bundleRoot}`);
  return violations;
}

if (
  process.argv[1] &&
  path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  const violations = checkCommercialBoundary({
    sourceRoot: "src",
    bundleRoot: process.argv.includes("--build")
      ? ".svelte-kit/output"
      : undefined,
  });
  if (violations.length) {
    console.error(`OSS commercial boundary failed:\n${violations.join("\n")}`);
    process.exitCode = 1;
  } else console.log("OSS commercial boundary: ok");
}
