/**
 * Canonical icon path data for shared UI glyphs.
 * Stroke icons use 24×24 viewBox unless noted.
 */

/** @type {Record<string, { d: string, fill?: 'currentColor'|'none', stroke?: boolean, fillRule?: string, clipRule?: string, paths?: string[] }>} */
export const ICONS = {
  trash: {
    d: "M14.74 9l-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 01-2.244 2.077H8.084a2.25 2.25 0 01-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 00-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 013.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 00-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 00-7.5 0",
    stroke: true,
  },
  archive: {
    fill: "currentColor",
    paths: [
      "M3.375 3C2.339 3 1.5 3.84 1.5 4.875v.75c0 1.036.84 1.875 1.875 1.875h17.25c1.035 0 1.875-.84 1.875-1.875v-.75C22.5 3.839 21.66 3 20.625 3H3.375Z",
      "m3.087 9 .54 9.176A3 3 0 0 0 6.62 21h10.757a3 3 0 0 0 2.995-2.824L20.913 9H3.087Zm6.163 3.75A.75.75 0 0 1 10 12h4a.75.75 0 0 1 0 1.5h-4a.75.75 0 0 1-.75-.75Z",
    ],
    fillRule: "evenodd",
    clipRule: "evenodd",
  },
  reply: {
    d: "M3 10h10a5 5 0 0 1 0 10M3 10l4-4M3 10l4 4",
    stroke: true,
  },
  replyTo: {
    d: "M9 14 4 9l5-5M4 9h11a5 5 0 0 1 5 5v0a5 5 0 0 1-5 5h-3",
    stroke: true,
  },
  close: {
    d: "M6 18 18 6M6 6l12 12",
    stroke: true,
  },
  chevronDown: {
    d: "M19 9l-7 7-7-7",
    stroke: true,
  },
  chevronLeft: {
    d: "M15 19l-7-7 7-7",
    stroke: true,
  },
  kebab: {
    d: "M12 6.75a.75.75 0 1 1 0-1.5.75.75 0 0 1 0 1.5ZM12 12.75a.75.75 0 1 1 0-1.5.75.75 0 0 1 0 1.5ZM12 18.75a.75.75 0 1 1 0-1.5.75.75 0 0 1 0 1.5Z",
    stroke: true,
  },
  link: {
    d: "M13.19 8.688a4.5 4.5 0 0 1 1.242 7.244l-4.5 4.5a4.5 4.5 0 0 1-6.364-6.364l1.757-1.757m13.35-.622 1.757-1.757a4.5 4.5 0 0 0-6.364-6.364l-4.5 4.5a4.5 4.5 0 0 0 1.242 7.244",
    stroke: true,
  },
  pencil: {
    d: "m16.862 4.487 1.687-1.688a1.875 1.875 0 1 1 2.652 2.652L10.582 16.07a4.5 4.5 0 0 1-1.897 1.13L6 18l.8-2.685a4.5 4.5 0 0 1 1.13-1.897l8.932-8.931Zm0 0L19.5 7.125M18 14v4.75A2.25 2.25 0 0 1 15.75 21H5.25A2.25 2.25 0 0 1 3 18.75V8.25A2.25 2.25 0 0 1 5.25 6H10",
    stroke: true,
  },
  info: {
    d: "m11.25 11.25.041-.02a.75.75 0 0 1 1.063.852l-.708 2.836a.75.75 0 0 0 1.063.853l.041-.021M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Zm-9-3.75h.008v.008H12V8.25Z",
    stroke: true,
  },
  comment: {
    d: "M8 12h8M8 8h8m-8 8h5m-9 3.5V6a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H9l-5 3.5Z",
    stroke: true,
  },
  spinner: {
    d: "M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z",
    fill: "currentColor",
  },
  check: {
    d: "M3.5 8.5 6.5 11.5 12.5 4.5",
    stroke: true,
    viewBox: "0 0 16 16",
  },
  xMark: {
    d: "M5 5l6 6M11 5l-6 6",
    stroke: true,
    viewBox: "0 0 16 16",
  },
  calendar: {
    d: "M8 2v2m8 4H4m13 8V8a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2Zm-11-2h4",
    stroke: true,
  },
  "grip-vertical": {
    d: "M9 5.25a1.125 1.125 0 1 1-2.25 0 1.125 1.125 0 0 1 2.25 0Zm0 4.5a1.125 1.125 0 1 1-2.25 0 1.125 1.125 0 0 1 2.25 0Zm0 4.5a1.125 1.125 0 1 1-2.25 0 1.125 1.125 0 0 1 2.25 0Zm4.5-9a1.125 1.125 0 1 1-2.25 0 1.125 1.125 0 0 1 2.25 0Zm0 4.5a1.125 1.125 0 1 1-2.25 0 1.125 1.125 0 0 1 2.25 0Zm0 4.5a1.125 1.125 0 1 1-2.25 0 1.125 1.125 0 0 1 2.25 0Z",
    fill: "currentColor",
  },
  external: {
    d: "M13.5 6H15v9a1.5 1.5 0 0 1-1.5 1.5H6M10 6h6m0 0v6m0-6L8.25 13.5",
    stroke: true,
  },
  filter: {
    d: "M3.75 5.25h16.5M6.75 12h10.5m-7.5 6.75h4.5",
    stroke: true,
  },
  sort: {
    d: "M3 7.5 7.5 3m0 0 4.5 4.5M7.5 3v13.5m13.5 0L16.5 21m0 0-4.5-4.5m4.5 4.5V7.5",
    stroke: true,
  },
};

/**
 * Shell navigation glyphs — one registry for the sidebar, the mobile bottom
 * bar and the /more hub. Drawn on a 24 viewBox at stroke 1.5 with round caps
 * and rendered at 16px. Every destination gets a distinct silhouette: two of
 * these used to share a path, so Integrations and Audit were the same icon.
 */
export const NAV_ICONS = {
  search: "M21 21l-4.35-4.35M17 10.5a6.5 6.5 0 11-13 0 6.5 6.5 0 0113 0z",
  askPm:
    "M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z",
  inbox:
    "M20 13V6a2 2 0 00-2-2H6a2 2 0 00-2 2v7m16 0v5a2 2 0 01-2 2H6a2 2 0 01-2-2v-5m16 0h-2.586a1 1 0 00-.707.293l-2.414 2.414a1 1 0 01-.707.293h-3.172a1 1 0 01-.707-.293l-2.414-2.414A1 1 0 006.586 13H4",
  tasks: "M4 6h.01M8 6h8M4 12h.01M8 12h8M4 18h.01M8 18h8",
  docs: "M14 3H7a2 2 0 00-2 2v14a2 2 0 002 2h10a2 2 0 002-2V8m-5-5l5 5m-5-5v5h5M9 13h6M9 17h4",
  access:
    "M16 19v-1a4 4 0 00-4-4H6a4 4 0 00-4 4v1M9 4a3 3 0 110 6 3 3 0 010-6zm13 15v-1a4 4 0 00-3-3.87M16 4.13a4 4 0 010 7.75",
  secrets:
    "M13 10.5a5.5 5.5 0 11-7.78 7.78A5.5 5.5 0 0113 10.5zM21 2l-9.6 9.6M15.5 7.5l3 3L22 7l-3-3",
  integrations: "M9 3v4M15 3v4M6 7h12v4a6 6 0 01-12 0V7zM12 17v4",
  audit: "M3 3v5h5M3.05 13A9 9 0 106 5.3L3 8M12 7.5V12l3 2",
  more: "M5 12h.01M12 12h.01M19 12h.01",
  account:
    "M13.5 6H5.25A2.25 2.25 0 003 8.25v10.5A2.25 2.25 0 005.25 21h10.5A2.25 2.25 0 0018 18.75V10.5M19.5 3v6m0 0h-6m6 0l-9 9",
  persona:
    "M12 12a4 4 0 100-8 4 4 0 000 8zM4 21v-1a6 6 0 016-6h2.5M16 18l2 2 4-4",
  signOut:
    "M15.75 9V5.25A2.25 2.25 0 0013.5 3h-6a2.25 2.25 0 00-2.25 2.25v13.5A2.25 2.25 0 007.5 21h6a2.25 2.25 0 002.25-2.25V15m3 0l3-3m0 0l-3-3m3 3H9",
};

/**
 * Path data for a shell nav glyph.
 *
 * An unknown key used to fall back to the inbox tray in silence, so a typo in
 * a nav entry shipped as "two destinations with the same icon". In dev the
 * miss is reported; the fallback still renders so the shell never breaks.
 *
 * @param {string} key
 * @returns {string}
 */
export function navIconPath(key) {
  const path = NAV_ICONS[key];
  if (path) {
    return path;
  }
  if (import.meta.env?.DEV) {
    console.error(
      `navIconPath: unknown icon key ${JSON.stringify(key)}. Add it to NAV_ICONS in src/lib/icons.js.`,
    );
  }
  return NAV_ICONS.inbox;
}
