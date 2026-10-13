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
          "createPmDecision",
          "createArtifactAttachment",
          "createWork",
          "createDocument",
          "patchDocument",
          "updateDocument",
          "setCardPlan",
          "updateBoardCard",
          "moveBoardCard",
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

it("task and document authoring surfaces checkpoint complete forms", () => {
  for (const [path, expected] of [
    ["routes/o/[organization]/w/[workspace]/tasks/new/+page.svelte", 1],
    ["lib/components/CommandPalette.svelte", 1],
    ["routes/o/[organization]/w/[workspace]/docs/+page.svelte", 1],
    [
      "routes/o/[organization]/w/[workspace]/docs/[documentId]/edit/+page.svelte",
      1,
    ],
    ["routes/o/[organization]/w/[workspace]/docs/[documentId]/+page.svelte", 2],
  ]) {
    const ast = parse(readFileSync(`${root}/${path}`, "utf8"));
    let actions = 0;
    walk(ast.html, (node) => {
      if (node.type === "Action" && node.name === "durableForm") actions++;
    });
    expect(actions).toBe(expected);
  }
});

it("task moves checkpoint full drafts and have no legacy direct sender", () => {
  for (const [path, action] of [
    ["routes/o/[organization]/w/[workspace]/tasks/+page.svelte", "durableForm"],
    ["lib/components/CommandPalette.svelte", "durableMoveForm"],
  ]) {
    const ast = parse(readFileSync(`${root}/${path}`, "utf8"));
    let count = 0;
    walk(ast.html, (node) => {
      if (node.type === "Action" && node.name === action) count++;
    });
    expect(count).toBe(1);
  }
  expect(readFileSync(`${root}/lib/taskBoardMove.js`, "utf8")).not.toMatch(
    /(?:moveBoardCard|createPmDecision|applyTaskPhaseMove)/,
  );
});
