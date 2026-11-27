/**
 * Answer POST /{kind}/lifecycle the way core does: each item runs through the
 * spec's own single-route handler, given a stand-in route for
 * POST /{kind}/{id}/{action} with the item's single-route body, and the
 * results come back in request order. When `available()` is false it answers
 * like a core without the route, so the client falls back to single sends.
 * Returns the batch request bodies, in order.
 */
export async function routeLifecycleBatch(
  context,
  kind,
  single,
  available = () => true,
) {
  const batches = [];
  await context.route(new RegExp(`/${kind}/lifecycle$`), async (route) => {
    const request = route.request().postDataJSON();
    batches.push(request);
    if (!available())
      return route.fulfill({
        status: 404,
        contentType: "application/json",
        body: JSON.stringify({
          error: { code: "not_found", message: "endpoint not found" },
        }),
      });
    const base = new URL(route.request().url());
    const prefix = base.pathname.replace(/\/lifecycle$/, "");
    const results = [];
    for (const [index, { id, action, ...body }] of request.items.entries()) {
      // Core applies the selection's bases to every item without its own.
      if (request.selected_bases && !body.selected_bases)
        body.selected_bases = request.selected_bases;
      const url = new URL(`${prefix}/${id}/${action}`, base).toString();
      let reply;
      let aborted;
      await single({
        request: () => ({
          url: () => url,
          method: () => "POST",
          postDataJSON: () => body,
          isNavigationRequest: () => false,
        }),
        fulfill: async (response) => {
          reply = response;
        },
        abort: async (reason) => {
          aborted = reason || "failed";
        },
      });
      // A dropped item drops the whole request, as a lost connection would.
      if (aborted) return route.abort(aborted);
      const status = reply?.status ?? 200;
      const payload = JSON.parse(reply?.body || "{}");
      results.push(
        status < 300
          ? { index, status: "done", http_status: status, response: payload }
          : {
              index,
              status: "failed",
              http_status: status,
              error: payload.error,
            },
      );
    }
    return route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ results }),
    });
  });
  return batches;
}
