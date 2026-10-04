/**
 * Which strings in a report can contain a ref, so a page can resolve all of
 * them in one request.
 *
 * A ref written in a table cell, a callout, a milestone or a diagram node is
 * just text in the panel contract — there is no `ref` field to read. So the
 * renderer scans the text it is about to draw, and this collects the same text
 * up front for a single batch resolve. One request per report, not one per
 * chip.
 */

const asText = (value) => String(value ?? "").trim();

/**
 * Every ref-bearing string in a report's panels, in panel order.
 *
 * @param {object[]} panels validated report panels
 * @returns {string[]}
 */
export function reportRefStrings(panels = []) {
  const out = [];
  const push = (value) => {
    const text = asText(value);
    if (text) out.push(text);
  };

  for (const panel of Array.isArray(panels) ? panels : []) {
    const data = panel?.data ?? {};
    switch (panel?.type) {
      case "callout":
      case "explanation":
        push(data.text);
        push(data.label);
        break;
      case "evidence-table":
        for (const row of Array.isArray(data.rows) ? data.rows : []) {
          for (const cell of Array.isArray(row?.cells) ? row.cells : []) {
            push(cell);
          }
        }
        break;
      case "milestone-timeline":
        for (const item of Array.isArray(data.items) ? data.items : []) {
          push(item?.label);
          push(item?.detail);
        }
        break;
      case "dependency-diagram":
        for (const node of Array.isArray(data.nodes) ? data.nodes : []) {
          push(node?.label);
        }
        for (const edge of Array.isArray(data.edges) ? data.edges : []) {
          push(edge?.label);
        }
        break;
      case "metric-strip":
        for (const item of Array.isArray(data.items) ? data.items : []) {
          push(item?.detail);
        }
        break;
      default:
        break;
    }
  }
  return out;
}
