export function buildProxyRequestInit(event, { body } = {}) {
  const headers = new Headers(event.request.headers);
  headers.delete("host");
  headers.delete("cookie");
  headers.delete("authorization");
  headers.set("x-forwarded-host", event.url.host);
  headers.set("x-forwarded-proto", event.url.protocol.replace(/:$/, ""));

  const method = event.request.method.toUpperCase();
  const requestInit = {
    method,
    headers,
  };

  if (method !== "GET" && method !== "HEAD") {
    requestInit.body = body ?? event.request.body;
    requestInit.duplex = "half";
  }

  return requestInit;
}
