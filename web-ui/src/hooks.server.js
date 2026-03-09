import { env } from "$env/dynamic/private";
import { isProxyableCommand } from "$lib/coreRouteCatalog";
import { PROJECT_HEADER, stripBasePath } from "$lib/projectPaths";
import { loadProjectCatalog } from "$lib/server/projectCatalog";
import { buildProxyRequestInit } from "$lib/server/coreProxy";
import { resolveProxyProjectTarget } from "$lib/server/proxyProjectTarget";

function shouldProxyToCore(pathname, method) {
  return isProxyableCommand(method, pathname);
}

function isDocumentNavigationRequest(request) {
  const method = request.method.toUpperCase();
  if (method !== "GET" && method !== "HEAD") {
    return false;
  }

  const secFetchDest = request.headers.get("sec-fetch-dest");
  if (secFetchDest === "document") {
    return true;
  }

  const accept = request.headers.get("accept") ?? "";
  return accept.includes("text/html");
}

function resolveProjectTarget(event) {
  const catalog = loadProjectCatalog(env);
  return resolveProxyProjectTarget({
    catalog,
    projectSlug: event.request.headers.get(PROJECT_HEADER),
  });
}

async function proxyToCore(event, coreBaseUrl) {
  const corePathname = stripBasePath(event.url.pathname);
  const targetUrl = new URL(
    `${corePathname}${event.url.search}`,
    `${coreBaseUrl}/`,
  ).toString();
  const requestInit = buildProxyRequestInit(event);

  let upstreamResponse;
  try {
    upstreamResponse = await fetch(targetUrl, requestInit);
  } catch (error) {
    const reason = error instanceof Error ? error.message : String(error);
    return new Response(
      JSON.stringify({
        error: {
          code: "core_unreachable",
          message: `Unable to reach oar-core at ${coreBaseUrl}. Start backend with ../core/scripts/dev and retry.`,
          reason,
        },
      }),
      {
        status: 503,
        headers: {
          "content-type": "application/json",
        },
      },
    );
  }
  const responseHeaders = new Headers(upstreamResponse.headers);
  responseHeaders.delete("content-encoding");
  responseHeaders.delete("content-length");

  return new Response(upstreamResponse.body, {
    status: upstreamResponse.status,
    statusText: upstreamResponse.statusText,
    headers: responseHeaders,
  });
}

export async function handle({ event, resolve }) {
  const pathname = stripBasePath(event.url.pathname);
  const method = event.request.method;
  const documentNavigation = isDocumentNavigationRequest(event.request);
  const proxyableRequest =
    shouldProxyToCore(pathname, method) && !documentNavigation;

  if (proxyableRequest) {
    const target = resolveProjectTarget(event);
    if (target.status) {
      return new Response(JSON.stringify(target.payload), {
        status: target.status,
        headers: {
          "content-type": "application/json",
        },
      });
    }

    if (target.coreBaseUrl) {
      return proxyToCore(event, target.coreBaseUrl);
    }
  }

  return resolve(event);
}
