<script>
  import { page } from "$app/stores";

  import Button from "$lib/components/Button.svelte";
  import {
    CALLBACK_COPY,
    CALLBACK_ERROR_SURFACE_CODES,
    callbackBodyForCode,
    callbackHeadingForCode,
  } from "$lib/hosted/callbackErrorCopy.js";

  let { data } = $props();

  let code = $derived($page.error?.code);
  let workspaceName = $derived($page.error?.workspace_name);

  let cpOrigin = $derived(
    String(data?.hostedCpOrigin ?? "").replace(/\/+$/, ""),
  );

  let dashboardHref = $derived(cpOrigin ? `${cpOrigin}/workspaces` : "");

  let isCallbackSurface = $derived(
    typeof code === "string" && CALLBACK_ERROR_SURFACE_CODES.has(code),
  );

  let heading = $derived.by(() => {
    if (!isCallbackSurface) {
      return "Something went wrong";
    }
    const h = callbackHeadingForCode(code);
    return h ?? CALLBACK_COPY.UNKNOWN.heading;
  });

  let bodyText = $derived.by(() => {
    if (!isCallbackSurface) {
      return $page.error?.message ?? "An unexpected error occurred.";
    }
    const b = callbackBodyForCode(code);
    return b ?? $page.error?.message ?? CALLBACK_COPY.UNKNOWN.body;
  });

  function tryAgain() {
    if (typeof window === "undefined") {
      return;
    }
    window.location.reload();
  }
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
    <h1 class="text-lg font-semibold text-[var(--fg)]">{heading}</h1>
    {#if workspaceName && isCallbackSurface}
      <p class="mt-1 text-[13px] font-medium text-[var(--fg-muted)]">
        {workspaceName}
      </p>
    {/if}
    <p class="mt-2 text-[13px] text-[var(--fg-muted)]">{bodyText}</p>

    {#if isCallbackSurface}
      <div
        class="mt-6 flex flex-col items-stretch gap-2 sm:flex-row sm:justify-center"
      >
        {#if code === "session_exchange_unreachable" || code === "workspace_core_unreachable"}
          <Button
            variant="primary"
            type="button"
            onclick={tryAgain}
            class="focus-visible:ring-2 focus-visible:ring-[var(--accent)] focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--bg)] focus-visible:outline-none"
          >
            Try again
          </Button>
          <Button
            variant="secondary"
            href={dashboardHref || undefined}
            class="focus-visible:ring-2 focus-visible:ring-[var(--accent)] focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--bg)] focus-visible:outline-none"
            disabled={!dashboardHref}
          >
            Return to dashboard
          </Button>
        {:else}
          <Button
            variant="primary"
            href={dashboardHref || undefined}
            class="focus-visible:ring-2 focus-visible:ring-[var(--accent)] focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--bg)] focus-visible:outline-none"
            disabled={!dashboardHref}
          >
            Return to dashboard
          </Button>
        {/if}
      </div>
    {:else}
      <p class="mt-3 text-[13px] text-[var(--fg-muted)]">
        The backend may be unavailable. Contact your administrator or check the
        service status.
      </p>
    {/if}

    {#if !isCallbackSurface}
      <details
        class="mt-6 rounded-md border border-[var(--line)] bg-[var(--bg-soft)] p-4 text-left"
      >
        <summary
          class="cursor-pointer text-[12px] font-medium text-[var(--fg-muted)]"
        >
          Technical troubleshooting
        </summary>
        <ol
          class="mt-2 list-decimal space-y-1.5 pl-5 text-[13px] text-[var(--fg-muted)]"
        >
          <li>
            Start the backend: <code
              class="rounded-md bg-[var(--line)] px-1.5 py-0.5 text-[11px] font-medium"
              >make serve</code
            >
            in
            <code
              class="rounded-md bg-[var(--line)] px-1.5 py-0.5 text-[11px] font-medium"
              >agent-nexus-core</code
            >
          </li>
          <li>
            Set <code
              class="rounded-md bg-[var(--line)] px-1.5 py-0.5 text-[11px] font-medium"
              >ANX_WORKSPACES='[&#123;"slug":"local","coreBaseUrl":"http://127.0.0.1:8000"&#125;]'</code
            >
            or the compatibility alias
            <code
              class="rounded-md bg-[var(--line)] px-1.5 py-0.5 text-[11px] font-medium"
              >ANX_PROJECTS</code
            >
          </li>
          <li>Reload this page.</li>
        </ol>
      </details>
    {/if}
  </div>
</div>
