import { createAnxCoreClient } from "$lib/anxCoreClient";
import { buildCoreRequestContextHeaders } from "$lib/coreClientRequestHeaders";
import { loadOverview, unavailableSection } from "$lib/overview.js";

export async function load(event) {
  const client = createAnxCoreClient({
    fetchFn: event.fetch,
    requestContextHeadersProvider: () =>
      buildCoreRequestContextHeaders({
        pathname: event.url.pathname,
      }),
  });
  try {
    return { overview: await loadOverview(client) };
  } catch (error) {
    const section = unavailableSection(error, "Overview could not be loaded.");
    return {
      overview: {
        needsYou: section,
        work: section,
        agents: section,
        reports: section,
      },
    };
  }
}
