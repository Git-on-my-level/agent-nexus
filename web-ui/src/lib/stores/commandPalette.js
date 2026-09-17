import { writable } from "svelte/store";

/**
 * Requests to open the workspace command palette from a page.
 *
 * Search left the mobile bottom bar: on desktop it is ⌘K, and every list
 * header carries a search button. Those buttons live outside the shell layout
 * that owns the palette, so they raise a counter the layout watches instead of
 * each page mounting its own palette.
 */
export const commandPaletteRequests = writable(0);

export function openCommandPalette() {
  commandPaletteRequests.update((count) => count + 1);
}
