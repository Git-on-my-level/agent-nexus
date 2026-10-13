function toSearchParams(searchParams) {
  if (searchParams instanceof URLSearchParams) {
    return searchParams;
  }

  return new URLSearchParams(searchParams);
}

export function readStringSearchParam(searchParams, key, defaultValue = "") {
  const raw = toSearchParams(searchParams).get(key);
  return String(raw ?? "").trim() || defaultValue;
}

export function readEnumSearchParam(
  searchParams,
  key,
  allowedValues,
  defaultValue = "",
) {
  const value = readStringSearchParam(searchParams, key, defaultValue);
  return allowedValues.includes(value) ? value : defaultValue;
}

/**
 * Like readEnumSearchParam, but maps legacy param values (e.g. `overview` → `about`)
 * before validation.
 *
 * @param {URLSearchParams} searchParams
 * @param {string} key
 * @param {string[]} allowedValues
 * @param {Record<string, string>} [aliases]
 * @param {string} [defaultValue]
 */
export function readEnumSearchParamWithAliases(
  searchParams,
  key,
  allowedValues,
  aliases = {},
  defaultValue = "",
) {
  let value = readStringSearchParam(searchParams, key, defaultValue);
  const mapped = aliases[value];
  if (mapped !== undefined) value = mapped;
  return allowedValues.includes(value) ? value : defaultValue;
}

export function buildSearchString(entries = {}) {
  const params = new URLSearchParams();

  for (const [key, value] of Object.entries(entries)) {
    if (Array.isArray(value)) {
      for (const item of value) {
        const normalized = String(item ?? "").trim();
        if (normalized) {
          params.append(key, normalized);
        }
      }
      continue;
    }

    const normalized = String(value ?? "").trim();
    if (normalized) {
      params.set(key, normalized);
    }
  }

  return params.toString();
}

export function withUpdatedSearchParams(url, updates = {}) {
  const next = new URL(url);

  for (const [key, value] of Object.entries(updates)) {
    next.searchParams.delete(key);

    if (Array.isArray(value)) {
      for (const item of value) {
        const normalized = String(item ?? "").trim();
        if (normalized) {
          next.searchParams.append(key, normalized);
        }
      }
      continue;
    }

    const normalized = String(value ?? "").trim();
    if (normalized) {
      next.searchParams.set(key, normalized);
    }
  }

  return `${next.pathname}${next.search}${next.hash}`;
}

/**
 * Overlay for a value written this turn before `$page` reflects it.
 * Sequential live-filter navigations each patch this instead of the stale URL.
 */
export function createInflightSlot() {
  let inflight = null;
  return {
    get(fallback) {
      return inflight ?? fallback;
    },
    set(next) {
      inflight = next;
      return next;
    },
    clear() {
      inflight = null;
    },
  };
}

/**
 * Copy `searchParams` and apply `changes` (empty values delete the key).
 * @param {string | URLSearchParams} searchParams
 * @param {Record<string, string>} [changes]
 */
export function patchSearchParams(searchParams, changes = {}) {
  const params = new URLSearchParams(searchParams);
  for (const [key, value] of Object.entries(changes)) {
    if (value) params.set(key, value);
    else params.delete(key);
  }
  return params;
}
