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
  onReturnFailure = () => {},
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
  let flight;
  function reportReturnFailure(error) {
    if (!valid() || !flight?.returning || flight.reported) return;
    flight.reported = true;
    onReturnFailure(error, flight.updatedAt);
  }
  const valid = () => !disposed && currentScope() === scope;
  function clear(error = "") {
    generation++;
    controller?.abort();
    pending = null;
    flight = null;
    state.set({ data: null, updatedAt: null, refreshing: false, error });
  }
  const stopDenied = onWorkspaceViewsDenied(scope, (error) => {
    reportReturnFailure(error);
    denied = true;
    clear(error?.message || "Read permission denied");
  });

  function setData(data, updatedAt = get(state).updatedAt) {
    if (!valid()) return;
    state.update((value) => ({ ...value, data, updatedAt, error: "" }));
    writeWorkspaceView(key, data, updatedAt ?? Date.now());
  }

  // Confirmed mutations must replace reads that started before the write.
  function refresh({ force = true, replace = false, returning = false } = {}) {
    if (!valid()) return Promise.resolve(false);
    if (pending && !replace) {
      if (returning) flight.returning = true;
      return pending;
    }
    if (replace) controller?.abort();
    const previous = get(state);
    if (
      !replace &&
      !force &&
      previous.updatedAt !== null &&
      Date.now() - previous.updatedAt < maxAgeMs
    )
      return Promise.resolve(false);
    denied = false;
    const ticket = ++generation;
    flight = { returning, updatedAt: previous.updatedAt, reported: false };
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
        if (valid() && ticket === generation && !signal.aborted) {
          reportReturnFailure(error);
          state.update((value) => ({
            ...value,
            error: error?.message || "Couldn’t refresh",
          }));
        }
        return false;
      } finally {
        if (valid() && ticket === generation)
          state.update((value) => ({ ...value, refreshing: false }));
        if (ticket === generation) {
          pending = null;
          flight = null;
        }
      }
    })();
    return pending;
  }

  /** Mount on the client. Reconnect subscriptions after a hidden/offline gap. */
  function mount({ connect = () => () => {} } = {}) {
    let stopLive = () => {};
    const resume = (force = false, returning = true) => {
      if (document.visibilityState === "hidden" || !valid()) return;
      stopLive();
      stopLive = connect(() => {
        if (!denied) return refresh();
      });
      if (!denied) void refresh({ force, returning });
    };
    const visibility = () => {
      if (document.visibilityState === "hidden") stopLive();
      else resume();
    };
    document.addEventListener("visibilitychange", visibility);
    const online = () => resume();
    window.addEventListener("online", online);
    resume(true, false);
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
