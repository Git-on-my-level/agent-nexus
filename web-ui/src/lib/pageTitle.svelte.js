/**
 * The document title has one owner. The root layout renders the only
 * `<title>`; a page names itself with `usePageTitle`. A `<title>` in a page's
 * own `<svelte:head>` never shows, because the browser uses the first one in
 * the document and the layout's comes first.
 *
 * The state lives in layout context rather than a module, so a server render
 * cannot leak one request's title into another.
 */
import { getContext, setContext } from "svelte";
import { derived, writable } from "svelte/store";

const PAGE_TITLE_KEY = Symbol.for("anx.page-title");

/**
 * A page that is being replaced can tear down after its successor has
 * already claimed the title, so a release only clears its own claim.
 */
export function createPageTitleState() {
  /** @type {import("svelte/store").Writable<{ owner: object, title: string } | null>} */
  const claim = writable(null);
  return {
    title: derived(claim, (current) => current?.title ?? ""),
    /** @param {object} owner @param {string} title */
    set(owner, title) {
      claim.set({ owner, title });
    },
    /** @param {object} owner */
    release(owner) {
      claim.update((current) => (current?.owner === owner ? null : current));
    },
  };
}

/** Call once from the root layout; render the returned store in `<title>`. */
export function providePageTitle() {
  const state = createPageTitleState();
  setContext(PAGE_TITLE_KEY, state);
  return state.title;
}

/**
 * Name the current page in the tab for as long as the calling component is
 * mounted. `getTitle` is reactive: the title follows the state it reads.
 *
 * @param {() => string | null | undefined} getTitle
 */
export function usePageTitle(getTitle) {
  /** @type {ReturnType<typeof createPageTitleState> | undefined} */
  const state = getContext(PAGE_TITLE_KEY);
  if (!state) return;
  const owner = {};
  $effect(() => {
    state.set(owner, String(getTitle() ?? "").trim());
  });
  $effect(() => () => state.release(owner));
}
