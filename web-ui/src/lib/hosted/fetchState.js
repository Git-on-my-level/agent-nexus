import { hostedCpFetch } from "$lib/hosted/cpFetch.js";

export class FetchError extends Error {
  constructor(kind, status, message) {
    super(message);
    this.kind = kind;
    this.status = status;
  }
}

export async function classifiedCpFetch(path, init) {
  let res;
  try {
    res = await hostedCpFetch(path, init);
  } catch {
    throw new FetchError(
      "network",
      0,
      "You're offline or the server is unreachable.",
    );
  }
  if (!res.ok) {
    let detail;
    try {
      const j = await res.json();
      detail = j?.error?.message || j?.error?.code || res.statusText;
    } catch {
      detail = res.statusText;
    }
    const kind =
      res.status >= 500 ? "server" : res.status === 401 ? "auth" : "client";
    throw new FetchError(kind, res.status, detail);
  }
  return res;
}

export function errorUserMessage(err) {
  if (err instanceof FetchError) return err.message;
  if (err instanceof Error) return err.message;
  return "Something went wrong.";
}

export function isNetworkError(err) {
  return err instanceof FetchError && err.kind === "network";
}

export function isServerError(err) {
  return err instanceof FetchError && err.kind === "server";
}

export function isAuthError(err) {
  return err instanceof FetchError && err.kind === "auth";
}
