import { get, writable } from "svelte/store";
import { reliableRead } from "./reliableRead.js";
import {
  readWorkspaceView,
  writeWorkspaceView,
  workspaceViewUpdatedAt,
  onWorkspaceViewsDenied,
} from "./workspaceViewCache.js";

/** A display snapshot belongs to exactly one principal/workspace tuple.
 * Reads coalesce, retain data on transient failure, and reject late results.
 * Auth denials revoke the snapshot rather than displaying private data.
 * Mount consumers inside the shell’s readerScope key; do not reuse an
 * instance after a principal or workspace switch.
 */
export function staleWhileRevalidate({
  scope,
  key,
  currentScope,
  read,
  beforeRead = () => {},
  onSuccess = () => {},
  maxAgeMs = 30_000,
  readOptions = {},
}) {
  const state = writable({
    data: readWorkspaceView(key),
    updatedAt: workspaceViewUpdatedAt(key),
    refreshing: false,
    error: "",
  });
  let controller;
  let pending;
  let disposed = false;
  let denied = false;
  let generation = 0;
  const valid = () => !disposed && currentScope() === scope;
  function clear(error = "") {
    generation++;
    controller?.abort();
    pending = null;
    state.set({ data: null, updatedAt: null, refreshing: false, error });
  }
  const stopDenied = onWorkspaceViewsDenied(scope, (error) => {
    denied = true;
    clear(error?.message || "Read permission denied");
  });

  function setData(data, updatedAt = get(state).updatedAt) {
    if (!valid()) return;
    state.update((value) => ({ ...value, data, updatedAt, error: "" }));
    writeWorkspaceView(key, data, updatedAt ?? Date.now());
  }

  function refresh({ force = true } = {}) {
    if (!valid()) return Promise.resolve(false);
    if (pending) return pending;
    const previous = get(state);
    if (
      !force &&
      previous.updatedAt !== null &&
      Date.now() - previous.updatedAt < maxAgeMs
    )
      return Promise.resolve(false);
    denied = false;
    const ticket = ++generation;
    controller = new AbortController();
    const signal = controller.signal;
    state.update((value) => ({
      ...value,
      refreshing: true,
      error: value.data ? "" : value.error,
    }));
    pending = (async () => {
      try {
        const data = await reliableRead(
          async (attemptSignal) => {
            await beforeRead();
            if (!valid() || attemptSignal.aborted)
              throw (
                attemptSignal.reason ||
                new DOMException("Reader changed", "AbortError")
              );
            return read(attemptSignal);
          },
          { ...readOptions, signal, cacheScope: scope },
        );
        if (!valid() || ticket !== generation) return false;
        setData(data, Date.now());
        onSuccess();
        return true;
      } catch (error) {
        if (valid() && ticket === generation && !signal.aborted)
          state.update((value) => ({
            ...value,
            error: error?.message || "Couldn’t refresh",
          }));
        return false;
      } finally {
        if (valid() && ticket === generation)
          state.update((value) => ({ ...value, refreshing: false }));
        pending = null;
      }
    })();
    return pending;
  }

  /** Mount on the client. Reconnect subscriptions after a hidden/offline gap. */
  function mount({ connect = () => () => {} } = {}) {
    let stopLive = () => {};
    const resume = (force = false) => {
      if (document.visibilityState === "hidden" || !valid()) return;
      stopLive();
      stopLive = connect(() => {
        if (!denied) void refresh();
      });
      if (!denied) void refresh({ force });
    };
    const visibility = () => {
      if (document.visibilityState === "hidden") stopLive();
      else resume();
    };
    document.addEventListener("visibilitychange", visibility);
    const online = () => resume();
    window.addEventListener("online", online);
    resume(true);
    return () => {
      stopLive();
      document.removeEventListener("visibilitychange", visibility);
      window.removeEventListener("online", online);
    };
  }
  return {
    subscribe: state.subscribe,
    refresh,
    setData,
    mount,
    destroy() {
      disposed = true;
      clear();
      stopDenied();
    },
  };
}
