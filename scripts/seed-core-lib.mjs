export function normalizeBaseUrl(value) {
  return String(value ?? "").trim().replace(/\/+$/, "");
}

export function parseJson(value) {
  const text = String(value ?? "").trim();
  if (!text) {
    return {};
  }

  try {
    return JSON.parse(text);
  } catch {
    return { raw: text };
  }
}

export function sleep(ms) {
  return new Promise((resolve) => {
    setTimeout(resolve, ms);
  });
}

export function failWithPrefix(prefix, message) {
  console.error(`${prefix}: ${message}`);
  process.exit(1);
}

export async function requestJson(
  baseUrl,
  method,
  requestPath,
  body,
  okStatuses = [200, 201],
) {
  const response = await fetch(`${baseUrl}${requestPath}`, {
    method,
    headers: {
      accept: "application/json",
      "content-type": "application/json",
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });

  const rawText = await response.text();
  const parsed = parseJson(rawText);
  if (!okStatuses.includes(response.status)) {
    const message =
      parsed?.error?.message ?? rawText ?? `${method} ${requestPath} failed`;
    throw new Error(`${method} ${requestPath} -> ${response.status}: ${message}`);
  }

  return parsed;
}

export async function waitForCore(
  baseUrl,
  timeoutMs,
  { probes = ["/version"], pollMs = 500 } = {},
) {
  const start = Date.now();

  while (Date.now() - start < timeoutMs) {
    try {
      const responses = await Promise.all(
        probes.map((probePath) => fetch(`${baseUrl}${probePath}`)),
      );
      if (responses.every((response) => response.ok)) {
        return;
      }
    } catch {
      // Ignore until timeout.
    }

    await sleep(pollMs);
  }

  throw new Error(
    `Timed out waiting for anx-core at ${baseUrl} after ${timeoutMs}ms.`,
  );
}
