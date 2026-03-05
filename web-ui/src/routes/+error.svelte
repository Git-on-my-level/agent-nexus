<script>
  import { page } from "$app/stores";
</script>

<div class="flex min-h-[60vh] items-center justify-center">
  <div class="max-w-md text-center">
    <h1 class="text-lg font-semibold text-gray-900">Unable to start</h1>
    <p class="mt-2 text-sm text-gray-500">
      {$page.error?.message ?? "An unexpected error occurred."}
    </p>
    <div
      class="mt-4 rounded-lg border border-gray-200 bg-white p-4 text-left text-sm text-gray-600"
    >
      <p class="text-xs font-semibold uppercase tracking-wider text-gray-400">
        Troubleshooting
      </p>
      <ol class="mt-2 list-decimal space-y-1 pl-5 text-sm">
        <li>
          Start the backend: <code
            class="rounded bg-gray-100 px-1.5 py-0.5 text-xs">make serve</code
          >
          in
          <code class="rounded bg-gray-100 px-1.5 py-0.5 text-xs"
            >organization-autorunner-core</code
          >
        </li>
        <li>
          Set <code class="rounded bg-gray-100 px-1.5 py-0.5 text-xs"
            >OAR_CORE_BASE_URL=http://127.0.0.1:8000</code
          >
        </li>
        <li>Reload this page.</li>
      </ol>
    </div>
  </div>
</div>
