import { runWorkspaceAuthCallbackPost } from "$lib/server/workspaceAuthCallbackPost.js";

function formDataFromSearchParams(url) {
  const form = new FormData();
  for (const [key, value] of url.searchParams.entries()) {
    form.append(key, value);
  }
  return form;
}

export async function POST(event) {
  return runWorkspaceAuthCallbackPost(event, {
    organizationSlug: event.params.organization,
    workspaceSlug: event.params.workspace,
  });
}

export async function GET(event) {
  return runWorkspaceAuthCallbackPost(
    event,
    {
      organizationSlug: event.params.organization,
      workspaceSlug: event.params.workspace,
    },
    formDataFromSearchParams(event.url),
  );
}
