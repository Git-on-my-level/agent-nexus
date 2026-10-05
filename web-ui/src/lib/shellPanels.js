/**
 * Collapsing the shell's side panels — the left nav and a page's right rail.
 *
 * Two rules, and the second is the one that is easy to get wrong:
 *
 * - **The viewer's choice is remembered.** Collapsing the nav is a preference,
 *   not a per-page mode, so it survives navigation and reload. It is stored
 *   per viewer: two people sharing a browser profile do not share a layout.
 * - **A narrow window overrides it.** Below the width a panel fits in, the
 *   panel is collapsed whatever the preference says, and the preference comes
 *   back when the window widens. Writing the override into storage instead
 *   would mean resizing a window silently rewrote a preference — expand the
 *   nav on a laptop, dock to a wide monitor, and it is still collapsed.
 *
 * Nothing here touches the DOM; a component owns `$state` and asks this
 * module what to show.
 */

const STORAGE_PREFIX = "anx:shell-panel";

/** Panels with a remembered collapse state. */
export const SHELL_PANELS = Object.freeze({
  NAV: "nav",
  RAIL: "rail",
});

/**
 * Widths below which a panel is collapsed regardless of the preference.
 *
 * These are the widths the panel has no room at, not widths someone guessed
 * looked cramped:
 *
 * - **nav, 1024px** — `lg`, where the shell swaps the sidebar for the bottom
 *   tab bar. Below it the sidebar is not on screen at all, so "collapsed" is
 *   simply true, and the toggle says so rather than pretending to act. At
 *   1024 itself the sidebar is 232px of a 1024px window and the remaining
 *   792px is a comfortable content column, so the viewer's choice rules from
 *   there up.
 * - **rail, none** — below `xl` a page's 18rem rail stacks under the content
 *   rather than sitting beside it, and stacked is not collapsed: the Source
 *   block and the Inbox link are still worth reading on a phone. Collapsing it
 *   there would hide content to save width the rail is no longer taking. So
 *   the rail follows the viewer's choice at every width.
 */
export const PANEL_AUTO_COLLAPSE_BELOW = Object.freeze({
  nav: 1024,
  rail: 0,
});

const asText = (value) => String(value ?? "").trim();

/**
 * Where one viewer's preference for one panel lives.
 *
 * An unidentified viewer (dev actor mode before a principal exists) gets a
 * shared key rather than no persistence: the alternative is a toggle that
 * forgets on every navigation.
 */
export function panelStorageKey(panel, viewerId = "") {
  const who = asText(viewerId) || "anonymous";
  return `${STORAGE_PREFIX}:${asText(panel)}:${who}`;
}

/**
 * @param {string} panel
 * @param {string} [viewerId]
 * @param {Storage} [storage]
 * @returns {boolean|null} the stored preference, or `null` when unset
 */
export function readPanelPreference(panel, viewerId = "", storage = undefined) {
  const store = storage ?? safeStorage();
  if (!store) return null;
  try {
    const raw = store.getItem(panelStorageKey(panel, viewerId));
    if (raw === "1") return true;
    if (raw === "0") return false;
    return null;
  } catch {
    return null;
  }
}

/**
 * @param {string} panel
 * @param {boolean} collapsed
 * @param {string} [viewerId]
 * @param {Storage} [storage]
 */
export function writePanelPreference(
  panel,
  collapsed,
  viewerId = "",
  storage = undefined,
) {
  const store = storage ?? safeStorage();
  if (!store) return;
  try {
    store.setItem(panelStorageKey(panel, viewerId), collapsed ? "1" : "0");
  } catch {
    // A full or blocked storage must not break the toggle; the state still
    // applies for this session.
  }
}

function safeStorage() {
  try {
    return typeof localStorage === "undefined" ? null : localStorage;
  } catch {
    // Safari in a blocked third-party context throws on access.
    return null;
  }
}

/**
 * Is the panel collapsed right now?
 *
 * @param {{ panel: string, preference?: boolean|null, viewportWidth?: number, defaultCollapsed?: boolean }} input
 */
export function panelCollapsed({
  panel,
  preference = null,
  viewportWidth = 0,
  defaultCollapsed = false,
}) {
  const threshold = PANEL_AUTO_COLLAPSE_BELOW[asText(panel)] ?? 0;
  const width = Number(viewportWidth) || 0;
  // A width of 0 means "not measured yet" (server render), not "very narrow".
  if (width > 0 && threshold > 0 && width < threshold) return true;
  return preference ?? defaultCollapsed;
}

/**
 * Is the automatic override what is collapsing the panel?
 *
 * The toggle says so rather than appearing broken: pressing "Expand" in a
 * window too narrow for the panel would do nothing visible.
 *
 * @param {{ panel: string, viewportWidth?: number }} input
 */
export function panelAutoCollapsed({ panel, viewportWidth = 0 }) {
  const threshold = PANEL_AUTO_COLLAPSE_BELOW[asText(panel)] ?? 0;
  const width = Number(viewportWidth) || 0;
  return width > 0 && threshold > 0 && width < threshold;
}
