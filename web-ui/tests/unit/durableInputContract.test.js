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
        [
          "respondInboxItem",
          "createEvent",
          "createPmConversation",
          "sendPmMessage",
          "answerPmDecision",
        ].includes(node.property?.name)
      ) {
        throw new Error(
          `${path.slice(root.length)} bypasses a durable mutation outbox`,
        );
      }
    });
  }
});

it("discussion text and metadata use a durable composer and outbox", () => {
  const ast = parse(
    readFileSync(`${root}/lib/components/timeline/MessagesTab.svelte`, "utf8"),
  );
  let composer = 0;
  walk(ast.html, (node) => {
    if (node.type !== "Element" || node.name !== "form") return;
    if (
      node.attributes.some(
        (attribute) =>
          attribute.type === "Action" && attribute.name === "durableForm",
      )
    )
      composer += 1;
  });
  expect(composer).toBe(1);
});

it("PM chat and decision notes use durable form snapshots", () => {
  for (const path of [
    "routes/o/[organization]/w/[workspace]/pm/+page.svelte",
    "lib/components/pm/DecisionPanel.svelte",
  ]) {
    const ast = parse(readFileSync(`${root}/${path}`, "utf8"));
    let forms = 0;
    walk(ast.html, (node) => {
      if (
        node.type === "Element" &&
        node.name === "form" &&
        node.attributes.some(
          (a) => a.type === "Action" && a.name === "durableForm",
        )
      )
        forms++;
    });
    expect(forms).toBe(1);
  }
});
