import { vi } from "vitest";

/**
 * Minimal {@link import('@sveltejs/kit').RequestEvent} stubs for route tests.
 */

/**
 * @param {Record<string, string>} formFields
 * @param {string} [url]
 * @param {{ organization?: string, workspace?: string }} [params]
 */
export function createFormPostEvent(
  formFields,
  url = "http://localhost/o/local/w/alpha/auth/callback",
  params = { organization: "local", workspace: "alpha" },
) {
  const form = new FormData();
  for (const [key, value] of Object.entries(formFields)) {
    form.set(key, value);
  }
  const cookieCalls = [];
  const event = {
    request: new Request(url, {
      method: "POST",
      body: form,
      headers: { accept: "application/json" },
    }),
    params: { ...params },
    url: new URL(url),
    cookies: {
      get: vi.fn(() => ""),
      set: vi.fn((name, value, options) => {
        cookieCalls.push({ name, value, options });
      }),
      delete: vi.fn(),
    },
    cookieCalls,
    getClientAddress: () => "127.0.0.1",
    fetch: globalThis.fetch,
  };
  return event;
}

/**
 * @param {string} url
 * @param {{ organization?: string, workspace?: string }} [params]
 */
export function createGetEvent(
  url = "http://localhost/auth/session",
  params = {},
) {
  return {
    request: new Request(url, {
      method: "GET",
      headers: { accept: "application/json" },
    }),
    params: { ...params },
    url: new URL(url),
    cookies: {
      get: vi.fn(() => ""),
      set: vi.fn(),
      delete: vi.fn(),
    },
    getClientAddress: () => "127.0.0.1",
    fetch: globalThis.fetch,
  };
}
