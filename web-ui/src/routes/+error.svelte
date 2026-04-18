<script>
  import { page } from "$app/stores";
</script>

<div class="flex min-h-[60vh] items-center justify-center">
  <div class="max-w-md text-center">
    <div
      class="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-red-500/10"
    >
      <svg
        class="h-6 w-6 text-red-400"
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
    <h1 class="text-lg font-semibold text-[var(--ui-text)]">
      Something went wrong
    </h1>
    <p class="mt-2 text-[13px] text-[var(--ui-text-muted)]">
      {$page.error?.message ?? "An unexpected error occurred."}
    </p>
    <p class="mt-3 text-[13px] text-[var(--ui-text-muted)]">
      The backend may be unavailable. Contact your administrator or check the
      service status.
    </p>
    <details
      class="mt-6 rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] p-4 text-left"
    >
      <summary
        class="cursor-pointer text-[12px] font-medium text-[var(--ui-text-muted)]"
      >
        Technical troubleshooting
      </summary>
      <ol
        class="mt-2 list-decimal space-y-1.5 pl-5 text-[13px] text-[var(--ui-text-muted)]"
      >
        <li>
          Start the backend: <code
            class="rounded-md bg-[var(--ui-border)] px-1.5 py-0.5 text-[11px] font-medium"
            >make serve</code
          >
          in
          <code
            class="rounded-md bg-[var(--ui-border)] px-1.5 py-0.5 text-[11px] font-medium"
            >agent-nexus-core</code
          >
        </li>
        <li>
          Set <code
            class="rounded-md bg-[var(--ui-border)] px-1.5 py-0.5 text-[11px] font-medium"
            >ANX_WORKSPACES='[&#123;"slug":"local","coreBaseUrl":"http://127.0.0.1:8000"&#125;]'</code
          >
          or the compatibility alias
          <code
            class="rounded-md bg-[var(--ui-border)] px-1.5 py-0.5 text-[11px] font-medium"
            >ANX_PROJECTS</code
          >
        </li>
        <li>Reload this page.</li>
      </ol>
    </details>
  </div>
</div>
