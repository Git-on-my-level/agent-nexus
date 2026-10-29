<script>
  import { dev } from "$app/environment";
  import { page } from "$app/stores";

  import { technicalTroubleshootingEnabled } from "$lib/extensions/supportLink.js";

  import Button from "$lib/components/Button.svelte";
  import { sameOriginAppPath } from "$lib/workspaceLaunchFlow.js";
  import {
    CALLBACK_COPY,
    CALLBACK_ERROR_SURFACE_CODES,
    callbackBodyForCode,
    callbackHeadingForCode,
  } from "$lib/workspaceCallbackErrorCopy.js";

  let { data } = $props();

  let errorObj = $derived($page.error ?? null);
  let status = $derived($page.status);
  let code = $derived(errorObj?.code);
  let workspaceName = $derived(errorObj?.workspace_name);
  // Same-origin sign-in offered when the viewer's login cannot open the
  // workspace. Anything that would resolve to another origin is dropped.
  let signInHref = $derived(
    sameOriginAppPath(errorObj?.signInUrl, $page.url?.origin),
  );

  let cpOrigin = $derived(
    String(data?.shellCapabilities?.publicOrigin ?? "").replace(/\/+$/, ""),
  );

  let dashboardHref = $derived(cpOrigin ? `${cpOrigin}/workspaces` : "");

  let isCallbackSurface = $derived(
    typeof code === "string" && CALLBACK_ERROR_SURFACE_CODES.has(code),
  );

  // Friendly headings/bodies for common workspace-resolution outcomes that
  // were previously falling through to the generic "Something went wrong"
  // copy. These are not callback-flow errors but they need actionable copy.
  const WORKSPACE_FRIENDLY_COPY = {
    workspace_not_ready: {
      heading: "Workspace is starting up",
      body: "The workspace exists but its compute backend isn't ready yet. This usually clears in a few seconds — try refreshing.",
      useServerMessage: true,
    },
    workspace_not_configured: {
      heading: "Workspace not found",
      body: "This server has no workspace at this address. Double-check the URL.",
      useServerMessage: true,
    },
    // The selected workspace answered 404 (deleted or moved). Definitive:
    // no sign-out, no Retry.
    workspace_not_found: {
      heading: "Workspace not found",
      body: "This workspace doesn't exist, or it was moved or removed. Check the link, or open another workspace.",
    },
    // Deliberately one answer for "does not exist" and "no access", so the
    // page does not tell a viewer which workspaces exist.
    workspace_not_accessible: {
      heading: "You don't have access to this workspace, or it doesn't exist",
      body: "Check the link, or sign in with an account that's a member of this workspace.",
    },
    workspace_lookup_unavailable: {
      heading: "We couldn't open this workspace",
      body: "Looking up the workspace failed. This is usually temporary — try again in a moment.",
    },
    workspace_route_incomplete: {
      heading: "Invalid workspace URL",
      body: "The URL is missing the organization or workspace segment.",
    },
    ssr_unhandled_error: {
      heading: "Unexpected server error",
      body: dev
        ? "An unhandled exception occurred while rendering the page. Check the dev terminal logs for a structured trace."
        : "We couldn’t load this page. Please try again in a moment.",
    },
  };

  let friendly = $derived(
    typeof code === "string" ? WORKSPACE_FRIENDLY_COPY[code] : undefined,
  );

  let heading = $derived.by(() => {
    if (isCallbackSurface) {
      const h = callbackHeadingForCode(code);
      return h ?? CALLBACK_COPY.UNKNOWN.heading;
    }
    if (friendly) {
      return friendly.heading;
    }
    return "Something went wrong";
  });

  let bodyText = $derived.by(() => {
    if (isCallbackSurface) {
      const b = callbackBodyForCode(code);
      return b ?? errorObj?.message ?? CALLBACK_COPY.UNKNOWN.body;
    }
    if (friendly) {
      // Codes whose server message carries detail (the workspace status, the
      // setting to fix) prefer it; the rest keep the page's own copy.
      return friendly.useServerMessage
        ? errorObj?.message || friendly.body
        : friendly.body;
    }
    return errorObj?.message ?? "An unexpected error occurred.";
  });

  function tryAgain() {
    if (typeof window === "undefined") {
      return;
    }
    window.location.reload();
  }

  // Show a primary "Try again" button for any code that's likely transient.
  let isTransient = $derived(
    code === "workspace_not_ready" ||
      code === "workspace_lookup_unavailable" ||
      code === "session_exchange_unreachable" ||
      code === "workspace_core_unreachable",
  );
</script>

<div class="flex min-h-[60vh] items-center justify-center px-4">
  <div class="max-w-md text-center">
    <div
      class="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-danger-soft"
    >
      <svg
        class="h-6 w-6 text-danger-text"
        fill="none"
        viewBox="0 0 24 24"
        stroke="currentColor"
        stroke-width="1.5"
      >
        <path
          stroke-linecap="round"
          stroke-linejoin="round"
          d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-2.5L13.732 4c-.77-.833-1.964-.833-2.732 0L4.082 16.5c-.77.833.192 2.5 1.732 2.5z"
        />
      </svg>
    </div>
    <h1 class="text-subtitle font-semibold text-fg">{heading}</h1>
    {#if workspaceName && isCallbackSurface}
      <p class="mt-1 text-meta font-medium text-fg-muted">
        {workspaceName}
      </p>
    {/if}
    <p class="mt-2 text-meta text-fg-muted">{bodyText}</p>

    {#if isCallbackSurface}
      <div
        class="mt-6 flex flex-col items-stretch gap-2 sm:flex-row sm:justify-center"
      >
        {#if code === "session_exchange_unreachable" || code === "workspace_core_unreachable"}
          <Button
            variant="primary"
            type="button"
            onclick={tryAgain}
            class="focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-bg focus-visible:outline-none"
          >
            Try again
          </Button>
          <Button
            variant="secondary"
            href={dashboardHref || undefined}
            class="focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-bg focus-visible:outline-none"
            disabled={!dashboardHref}
          >
            Return to dashboard
          </Button>
        {:else}
          <Button
            variant="primary"
            href={dashboardHref || undefined}
            class="focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-bg focus-visible:outline-none"
            disabled={!dashboardHref}
          >
            Return to dashboard
          </Button>
        {/if}
      </div>
    {:else if code === "workspace_not_accessible"}
      <div
        class="mt-6 flex flex-col items-stretch gap-2 sm:flex-row sm:justify-center"
      >
        {#if signInHref}
          <Button
            variant="primary"
            href={signInHref}
            class="focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-bg focus-visible:outline-none"
          >
            Switch account
          </Button>
        {/if}
        {#if dashboardHref}
          <Button
            variant={signInHref ? "secondary" : "primary"}
            href={dashboardHref}
            class="focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-bg focus-visible:outline-none"
          >
            Return to dashboard
          </Button>
        {/if}
      </div>
    {:else if isTransient || !friendly}
      <div
        class="mt-6 flex flex-col items-stretch gap-2 sm:flex-row sm:justify-center"
      >
        <Button
          variant="primary"
          type="button"
          onclick={tryAgain}
          class="focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-bg focus-visible:outline-none"
        >
          Try again
        </Button>
        {#if dashboardHref}
          <Button
            variant="secondary"
            href={dashboardHref}
            class="focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-bg focus-visible:outline-none"
          >
            Return to dashboard
          </Button>
        {/if}
      </div>
    {/if}
    {#if !isCallbackSurface && !friendly}
      <p class="mt-3 text-[13px] text-fg-muted">
        The backend may be unavailable. Contact your administrator or check the
        service status.
      </p>
    {/if}

    {#if dev}
      <details
        class="mt-6 rounded-md border border-line bg-bg-soft p-4 text-left"
        open
      >
        <summary class="cursor-pointer text-[12px] font-medium text-fg-muted">
          Dev diagnostics
        </summary>
        <dl
          class="mt-2 grid grid-cols-[max-content_1fr] gap-x-3 gap-y-1 text-[12px] text-fg-muted"
        >
          <dt class="font-medium">Status</dt>
          <dd>
            <code
              class="rounded-md bg-line px-1.5 py-0.5 text-[11px] font-medium"
              >{status ?? "(none)"}</code
            >
          </dd>
          <dt class="font-medium">Code</dt>
          <dd>
            <code
              class="rounded-md bg-line px-1.5 py-0.5 text-[11px] font-medium"
              >{code ?? "(none)"}</code
            >
          </dd>
          <dt class="font-medium">URL</dt>
          <dd>
            <code
              class="break-all rounded-md bg-line px-1.5 py-0.5 text-[11px] font-medium"
              >{$page.url?.pathname ?? "(unknown)"}{$page.url?.search ??
                ""}</code
            >
          </dd>
          <dt class="font-medium">Route</dt>
          <dd>
            <code
              class="rounded-md bg-line px-1.5 py-0.5 text-[11px] font-medium"
              >{$page.route?.id ?? "(unknown)"}</code
            >
          </dd>
          <dt class="font-medium">Message</dt>
          <dd class="break-words">{errorObj?.message ?? "(none)"}</dd>
        </dl>
        <p class="mt-3 text-[11px] text-fg-muted">
          A structured trace was logged to the dev terminal — search for <code
            class="rounded-md bg-line px-1 text-[11px] font-medium">[anx]</code
          >
          in your
          <code class="rounded-md bg-line px-1 text-[11px] font-medium"
            >make serve</code
          > output. This panel only renders in dev builds.
        </p>
      </details>
    {/if}

    {#if technicalTroubleshootingEnabled && !isCallbackSurface && !friendly}
      <details
        class="mt-6 rounded-md border border-line bg-bg-soft p-4 text-left"
      >
        <summary class="cursor-pointer text-[12px] font-medium text-fg-muted">
          Technical troubleshooting
        </summary>
        <ol
          class="mt-2 list-decimal space-y-1.5 pl-5 text-[13px] text-fg-muted"
        >
          <li>
            Start the backend: <code
              class="rounded-md bg-line px-1.5 py-0.5 text-[11px] font-medium"
              >make serve</code
            >
            in
            <code
              class="rounded-md bg-line px-1.5 py-0.5 text-[11px] font-medium"
              >agent-nexus-core</code
            >
          </li>
          <li>
            Set <code
              class="rounded-md bg-line px-1.5 py-0.5 text-[11px] font-medium"
              >ANX_WORKSPACES='[&#123;"organizationSlug":"local","slug":"local","coreBaseUrl":"http://127.0.0.1:8000"&#125;]'</code
            >
          </li>
          <li>Reload this page.</li>
        </ol>
      </details>
    {/if}
  </div>
</div>
