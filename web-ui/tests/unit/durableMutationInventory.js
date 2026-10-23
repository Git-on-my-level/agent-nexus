import { readdirSync, readFileSync } from "node:fs";
import { parse } from "svelte/compiler";
import { commandRegistry } from "../../../contracts/gen/ts/dist/client.js";

function walk(node, visit, parent = null) {
  if (!node || typeof node !== "object") return;
  visit(node, parent);
  for (const value of Object.values(node)) {
    if (Array.isArray(value))
      value.forEach((child) => walk(child, visit, node));
    else if (value && typeof value === "object") walk(value, visit, node);
  }
}

// This is a finite review boundary, not proof of arbitrary JavaScript dataflow.
// Include registered POST read helpers and auth controls: exemptions must be
// explicit too. Never infer that every POST is safe for automatic recovery.
export function mutationMethods(adapterSource) {
  const writes = commandRegistry.filter((command) => command.method !== "GET");
  const methods = new Set(
    writes.flatMap((command) => [command.operation_id, command.ts_method]),
  );
  const ast = parse(`<script>${adapterSource}</script>`);
  walk(ast.instance, (node) => {
    if (node.type !== "ArrayExpression") return;
    const [name, command] = node.elements;
    if (writes.some((write) => write.command_id === command?.value)) {
      methods.add(name?.value);
    }
  });
  return methods;
}

export function mutationReferences(
  source,
  svelte,
  methods,
  typescript = false,
) {
  const ast = parse(
    svelte
      ? source
      : `<script${typescript ? ' lang="ts"' : ""}>${source}</script>`,
  );
  const hits = {};
  function record(name) {
    hits[name] = (hits[name] ?? 0) + 1;
  }
  walk(ast, (node, parent) => {
    // A destructured sender can be renamed before it is called. Count the
    // binding itself, like client.method references, rather than its alias.
    if (node.type === "ObjectPattern") {
      for (const property of node.properties) {
        const name = property.computed
          ? property.key?.value
          : (property.key?.name ?? property.key?.value);
        if (methods.has(name)) record(name);
        if (name === "fetch") record("raw-fetch");
        if (property.computed && property.key?.type !== "Literal")
          record("computed-dispatch");
      }
    }
    if (
      node.type === "Property" &&
      parent?.type !== "ObjectPattern" &&
      node.value?.type === "Identifier" &&
      node.value.name === "fetch"
    )
      record("raw-fetch");
    if (
      node.type === "Identifier" &&
      node.name === "fetch" &&
      parent?.callee !== node &&
      parent?.type !== "Property" &&
      !(
        parent?.type === "MemberExpression" &&
        parent.property === node &&
        !parent.computed
      ) &&
      !(parent?.type === "VariableDeclarator" && parent.id === node) &&
      ![
        "ImportSpecifier",
        "FunctionDeclaration",
        "ArrowFunctionExpression",
        "FunctionExpression",
      ].includes(parent?.type)
    )
      record("raw-fetch");
    if (node.type === "MemberExpression") {
      const name = node.computed ? node.property?.value : node.property?.name;
      if (methods.has(name)) record(name);
      if (name === "fetch") record("raw-fetch");
      if (
        node.computed &&
        node.property?.type !== "Literal" &&
        node.object?.name === "coreClient" &&
        parent?.callee !== node
      )
        record("computed-dispatch");
    }
    if (node.type !== "CallExpression") return;
    const callee = node.callee;
    if (callee?.type === "Identifier") {
      if (methods.has(callee.name)) record(callee.name);
      if (
        [
          "fetch",
          "fetchFn",
          "baseFetchFn",
          "requestJSON",
          "invokeCommand",
          "invokeDirectRaw",
        ].includes(callee.name)
      ) {
        record(`transport:${callee.name}`);
      }
    }
    if (
      callee?.type === "MemberExpression" &&
      callee.computed &&
      callee.property?.type !== "Literal"
    ) {
      record("computed-dispatch");
    }
  });
  return Object.fromEntries(Object.entries(hits).sort());
}

export function browserMutationInventory(root) {
  const methods = mutationMethods(
    readFileSync(`${root}/lib/anxCoreClient.js`, "utf8"),
  );
  const inventory = {};
  function scan(dir) {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      const path = `${dir}/${entry.name}`;
      if (entry.isDirectory()) {
        if (entry.name !== "server") scan(path);
        continue;
      }
      if (
        !/\.(?:[cm]?js|ts|svelte)$/.test(path) ||
        /(?:\+server|\.server)\.(?:[cm]?js|ts)$/.test(path)
      ) {
        continue;
      }
      const references = mutationReferences(
        readFileSync(path, "utf8"),
        path.endsWith(".svelte"),
        methods,
        path.endsWith(".ts"),
      );
      if (Object.keys(references).length) {
        inventory[path.slice(root.length + 1)] = references;
      }
    }
  }
  scan(root);
  return Object.fromEntries(Object.entries(inventory).sort());
}
