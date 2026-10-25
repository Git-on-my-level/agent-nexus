/**
 * Which Watch tab and layout a URL asks for.
 *
 * `?tab=initiatives|loops|dashboard` picks the tab; `?view=` picks the
 * layout inside it. Initiatives lay out as a list or as the swimlane board
 * (`?view=lanes`); Loops as a list or a board of state columns
 * (`?view=board`). Links already sent out still land:
 *
 * - `?view=board` and `?layout=board` were the by-stage board. On
 *   Initiatives they open the lanes, which replaced it.
 * - `?view=loops` was the Loops tab.
 * - `/initiatives?view=lanes` redirects here as `?view=lanes`.
 */

export const WATCH_TABS = Object.freeze(["initiatives", "loops", "dashboard"]);

/** The non-list layout each tab offers. */
export const WATCH_BOARD = Object.freeze({
  initiatives: "lanes",
  loops: "board",
});

/**
 * @param {URLSearchParams} params
 * @returns {{ tab: string, layout: "list" | "lanes" | "board" }}
 */
export function watchView(params) {
  const view = params.get("view") || "";
  const wanted = params.get("tab") || "";
  const tab = WATCH_TABS.includes(wanted)
    ? wanted
    : view === "loops"
      ? "loops"
      : params.get("dashboard")
        ? "dashboard"
        : "initiatives";
  const board =
    view === "lanes" || view === "board" || params.get("layout") === "board";
  return { tab, layout: board ? (WATCH_BOARD[tab] ?? "list") : "list" };
}

/**
 * The query patch that shows `layout` on the current tab. Every legacy key
 * is cleared, so the URL only ever carries the current spelling.
 */
export function layoutPatch(layout) {
  return { view: layout === "list" ? "" : layout, layout: "" };
}
