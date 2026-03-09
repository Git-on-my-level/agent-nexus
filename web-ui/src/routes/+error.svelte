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
    <h1 class="text-lg font-semibold text-[var(--ui-text)]">Unable to start</h1>
    <p class="mt-2 text-[13px] text-[var(--ui-text-muted)]">
      {$page.error?.message ?? "An unexpected error occurred."}
    </p>
    <div
      class="mt-6 rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] p-4 text-left"
    >
      <p class="text-[12px] font-medium text-[var(--ui-text-muted)]">
        Troubleshooting
      </p>
      <ol
        class="mt-2 list-decimal space-y-1.5 pl-5 text-[13px] text-[var(--ui-text-muted)]"
      >
        <li>
          Start the backend: <code
            class="rounded-md bg-[var(--ui-border)] px-1.5 py-0.5 text-xs font-medium"
            >make serve</code
          >
          in
          <code
            class="rounded-md bg-[var(--ui-border)] px-1.5 py-0.5 text-xs font-medium"
            >organization-autorunner-core</code
          >
        </li>
        <li>
          Set <code
            class="rounded-md bg-[var(--ui-border)] px-1.5 py-0.5 text-xs font-medium"
            >OAR_PROJECTS='[&#123;"slug":"local","coreBaseUrl":"http://127.0.0.1:8000"&#125;]'</code
          >
        </li>
        <li>Reload this page.</li>
      </ol>
    </div>
  </div>
</div>
