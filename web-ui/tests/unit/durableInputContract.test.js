import { readFileSync, readdirSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { parse } from "svelte/compiler";
import { expect, it } from "vitest";

const root = fileURLToPath(new URL("../../src/", import.meta.url));
function* files(dir) {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = `${dir}/${entry.name}`;
    if (entry.isDirectory()) yield* files(path);
    else if (entry.name.endsWith(".svelte")) yield path;
  }
}
function walk(node, visit) {
  if (!node || typeof node !== "object") return;
  if (node.type) visit(node);
  for (const value of Object.values(node)) {
    if (Array.isArray(value)) for (const child of value) walk(child, visit);
    else if (value && typeof value === "object") walk(value, visit);
  }
}

it("every authored text field in the Inbox response form uses durable drafts", () => {
  const ast = parse(
    readFileSync(
      `${root}/lib/components/inbox/InboxRespondPanel.svelte`,
      "utf8",
    ),
  );
  let fields = 0;
  walk(ast.html, (node) => {
    if (node.type !== "Element" || node.name !== "textarea") return;
    fields += 1;
    expect(
      node.attributes.some(
        (attribute) =>
          attribute.type === "Action" && attribute.name === "durableDraft",
      ),
    ).toBe(true);
  });
  expect(fields).toBe(2);
});

it("no UI mutation surface bypasses the Inbox outbox sender", () => {
  for (const path of files(root)) {
    const ast = parse(readFileSync(path, "utf8"));
    walk(ast.instance, (node) => {
      if (
        node.type === "MemberExpression" &&
        node.property?.name === "respondInboxItem"
      ) {
        throw new Error(
          `${path.slice(root.length)} bypasses the durable Inbox outbox`,
        );
      }
    });
  }
});
