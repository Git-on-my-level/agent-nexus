<script>
  import { page } from "$app/stores";
</script>

<div class="rounded-lg border border-rose-200 bg-rose-50 p-6 text-rose-900">
  <h1 class="text-2xl font-semibold">
    Unable to start Organization Autorunner UI
  </h1>
  <p class="mt-3 whitespace-pre-wrap text-sm leading-6">
    {$page.error?.message ?? "An unexpected startup error occurred."}
  </p>
  <div class="mt-4 rounded-md border border-rose-200 bg-white/70 p-4">
    <p class="text-xs font-semibold uppercase tracking-wide">Try this</p>
    <ol class="mt-2 list-decimal space-y-1 pl-5 text-sm">
      <li>
        Start backend: <code
          >cd ../organization-autorunner-core && ./scripts/dev</code
        >
      </li>
      <li>
        Set UI core URL:
        <code>OAR_CORE_BASE_URL=http://127.0.0.1:8000</code>
      </li>
      <li>Reload this page after core is reachable.</li>
    </ol>
  </div>
</div>
