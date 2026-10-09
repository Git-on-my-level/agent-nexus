/**
 * One clock for every relative time on the page.
 *
 * Each `<Time>` retains this clock instead of starting its own timer. The
 * interval exists only while something is showing a live time, and it ticks
 * once every 30 seconds — often enough that "15 min ago" does not sit stale
 * for a whole minute, rare enough that a dashboard of badges is not a
 * hundred timers.
 *
 * The displayed instant is always `Date.now()`. `generation` exists only so
 * a Svelte read of `clockNow()` subscribes to the tick. On the server there
 * is no interval, and each render reads the clock fresh.
 *
 * @eslint no-undef: Svelte 5 compiler provides $state in `.svelte.js`.
 */
/* eslint-disable no-undef -- Svelte 5 runes */

/** How often a live relative label is allowed to change. */
export const TIME_TICK_MS = 30_000;

let generation = $state(0);
let subscribers = 0;
let timer = 0;

export function clockNow() {
  void generation;
  return Date.now();
}

/**
 * Hold the shared interval open until the returned function runs.
 * A second caller does not start a second interval.
 */
export function retainClock() {
  subscribers += 1;
  if (subscribers === 1 && typeof window !== "undefined") {
    timer = window.setInterval(() => {
      generation += 1;
    }, TIME_TICK_MS);
  }
  return () => {
    subscribers = Math.max(0, subscribers - 1);
    if (subscribers > 0 || !timer) return;
    window.clearInterval(timer);
    timer = 0;
  };
}
