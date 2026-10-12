import { recoverStaleChunk } from "$lib/chunkRecovery.js";

// Navigation import failures go through Kit; Vite preloads use this event.
if (typeof window !== "undefined") {
  window.addEventListener("vite:preloadError", (event) => {
    if (recoverStaleChunk(event.payload)) event.preventDefault();
  });
  window.addEventListener("unhandledrejection", (event) => {
    if (recoverStaleChunk(event.reason)) event.preventDefault();
  });
}

export function handleError({ error, message }) {
  recoverStaleChunk(error);
  return { message: message || "This page could not be loaded. Please retry." };
}
