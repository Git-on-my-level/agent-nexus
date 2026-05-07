/**
 * Rasterize static/favicon.svg to PNGs for PWA / iOS (centered, full canvas).
 * Run from repo: `pnpm run generate:icons` (in web-ui/).
 */
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "@playwright/test";

const root = path.join(path.dirname(fileURLToPath(import.meta.url)), "..");
const staticDir = path.join(root, "static");
const svgRaw = fs.readFileSync(path.join(staticDir, "favicon.svg"), "utf8");

function svgAtSize(size) {
  return svgRaw.replace("<svg ", `<svg width="${size}" height="${size}" `);
}

const outputs = [
  ["apple-touch-icon.png", 180],
  ["icon-192.png", 192],
  ["icon-512.png", 512],
];

async function main() {
  const browser = await chromium.launch();
  const page = await browser.newPage({ deviceScaleFactor: 1 });

  try {
    for (const [name, size] of outputs) {
      const html = `<!DOCTYPE html><html><head><meta charset="utf-8"><style>html,body{margin:0;padding:0;width:${size}px;height:${size}px;overflow:hidden}</style></head><body>${svgAtSize(size)}</body></html>`;
      await page.setViewportSize({ width: size, height: size });
      await page.setContent(html, { waitUntil: "networkidle" });
      await page.screenshot({
        path: path.join(staticDir, name),
        type: "png",
        clip: { x: 0, y: 0, width: size, height: size },
      });
      process.stdout.write(`wrote static/${name} (${size}×${size})\n`);
    }
  } finally {
    await browser.close();
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
