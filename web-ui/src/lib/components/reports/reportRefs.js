/**
 * Which strings in a report can contain a ref, so a page can resolve all of
 * them in one request.
 *
 * A ref written in a table cell, a callout, a milestone or a diagram node is
 * just text in the panel contract — there is no `ref` field to read. So the
 * renderer scans the text it is about to draw, and this collects the same text
 * up front for a single batch resolve. One request per report, not one per
 * chip.
 *
 * Live panels have their text twice over: the authored panel holds a query,
 * and the observation that answers it holds the prose. Only the observation
 * has refs in it, and it arrives after the page does — so collection has to
 * read the observed panel, not the authored one, and run again when the
 * observation changes.
 */

const asText = (value) => String(value ?? "").trim();

/**
 * Every ref-bearing string in a report's panels, in panel order.
 *
 * Pass observed panels rather than authored ones: for a live panel the refs
 * are in the observation, and an authored live panel carries only its query.
 *
 * @param {object[]} panels validated report panels, ideally observed
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
    pushLiveRefStrings(panel, push);
    switch (panel?.type) {
      case "callout":
      case "explanation":
        push(data.text);
        push(data.label);
        break;
      // Both spellings: the renderer chips either, so collection has to ask for
      // either. A ref only ever written in a `table` was rendering "not found"
      // because it was never requested.
      case "table":
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

/**
 * The ref-bearing strings in a live panel's observation.
 *
 * Only the strings the renderer actually chips. An initiative's summary goes
 * through `MarkdownRenderer` with the resolved map, so a ref written in one is
 * a chip; its needs and assignees are drawn as plain text and are not. A ref
 * that appears nowhere but a live summary used to render dashed and "not
 * found" — it was never in the batch, because the batch was built from the
 * authored panels and an authored live panel is a query with no prose in it.
 *
 * `item.ref` is included too: the resolved map is what gives a step chip in
 * the same panel its title and status.
 *
 * @param {object} panel an observed panel (`panel.live.data` is the reading)
 * @param {(value: unknown) => void} push
 */
function pushLiveRefStrings(panel, push) {
  if (panel?.type !== "live-initiatives") return;
  const live = panel?.live;
  if (live?.status !== "ok") return;
  for (const item of Array.isArray(live.data?.items) ? live.data.items : []) {
    push(item?.ref);
    // Prose only: with `summary=1` the row's `summary` is the computed
    // object, whose stringification names no refs.
    push(typeof item?.summary === "string" ? item.summary : item?.summary_text);
  }
}
