// Layout is a bounded presentation tree. It cannot name executable components.
export function validateReportLayout(layout, panelIds) {
  const errors = [];
  const panels = new Set();
  const tabs = new Set();
  let count = 0;
  const id = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,79}$/;
  const add = (path, message) => {
    if (errors.length < 20) errors.push(`${path}: ${message}`);
  };
  const text = (value, path, max = 200) => {
    if (typeof value !== "string" || !value.trim() || value.length > max)
      add(path, `requires nonempty text of at most ${max} characters`);
  };
  const record = (node, path, required, optional = []) => {
    if (!node || typeof node !== "object" || Array.isArray(node)) {
      add(path, "must be an object");
      return false;
    }
    const keys = [...required, ...optional];
    if (
      required.some((key) => !Object.hasOwn(node, key)) ||
      Object.keys(node).some((key) => !keys.includes(key))
    )
      add(path, "missing or unsupported fields");
    return true;
  };
  const children = (value, path, depth) => {
    if (!Array.isArray(value) || !value.length || value.length > 32) {
      add(path, "requires 1–32 children");
      return;
    }
    value.forEach((child, index) => visit(child, `${path}[${index}]`, depth));
  };
  const visit = (node, path, depth) => {
    if (++count > 100 || depth > 6) {
      add(path, "layout exceeds 100 nodes or 6 levels");
      return;
    }
    if (!node || typeof node !== "object" || Array.isArray(node)) {
      add(path, "must be an object");
      return;
    }
    if (
      node.span !== undefined &&
      (!Number.isInteger(node.span) || node.span < 1 || node.span > 4)
    )
      add(path, "span must be 1–4");
    switch (node.type) {
      case "panel":
        record(node, path, ["type", "panel_id"], ["span"]);
        if (!panelIds.has(node.panel_id))
          add(path, "references a missing panel");
        if (panels.has(node.panel_id))
          add(path, "panel references must be unique");
        panels.add(node.panel_id);
        break;
      case "stack":
        record(node, path, ["type", "children"], ["span"]);
        children(node.children, `${path}.children`, depth + 1);
        break;
      case "grid":
        // columns is an optional cap, not a requirement: a grid that names
        // none flows its children by available width and content.
        record(node, path, ["type", "children"], ["span", "columns"]);
        if (node.columns !== undefined && ![2, 3, 4].includes(node.columns))
          add(path, "columns must be 2, 3, or 4");
        children(node.children, `${path}.children`, depth + 1);
        break;
      case "section":
      case "disclosure":
        record(
          node,
          path,
          ["type", "title", "children"],
          node.type === "section" ? ["span", "description"] : ["span", "open"],
        );
        text(node.title, `${path}.title`);
        if (node.description !== undefined)
          text(node.description, `${path}.description`, 2000);
        if (node.open !== undefined && typeof node.open !== "boolean")
          add(path, "open must be a boolean");
        children(node.children, `${path}.children`, depth + 1);
        break;
      case "tabs": {
        record(node, path, ["type", "id", "items"], ["span"]);
        if (
          typeof node.id !== "string" ||
          !id.test(node.id) ||
          tabs.has(node.id)
        )
          add(path, "tab groups need unique identifiers");
        tabs.add(node.id);
        if (
          !Array.isArray(node.items) ||
          !node.items.length ||
          node.items.length > 8
        ) {
          add(path, "tabs require 1–8 items");
          break;
        }
        const items = new Set();
        node.items.forEach((item, index) => {
          const itemPath = `${path}.items[${index}]`;
          if (!record(item, itemPath, ["id", "label", "children"])) return;
          if (
            typeof item.id !== "string" ||
            !id.test(item.id) ||
            items.has(item.id)
          )
            add(itemPath, "tab items need unique identifiers");
          items.add(item.id);
          text(item.label, `${itemPath}.label`);
          children(item.children, `${itemPath}.children`, depth + 1);
        });
        break;
      }
      default:
        add(path, "unsupported layout type");
    }
  };
  visit(layout, "root", 0);
  return errors;
}
