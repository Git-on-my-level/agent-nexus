<script>
  import "../app.css";
  import { navigationItems } from "$lib/navigation";
</script>

<div class="min-h-screen bg-slate-50 text-slate-900">
  <div class="mx-auto flex min-h-screen max-w-6xl">
    <aside class="w-64 border-r border-slate-200 bg-white p-6">
      <p class="text-xs font-semibold uppercase tracking-wide text-slate-500">
        Navigation
      </p>
      <nav class="mt-4" aria-label="Primary">
        <ul class="space-y-2">
          {#each navigationItems as item}
            <li>
              <a
                class="block rounded-md px-3 py-2 text-sm font-medium text-slate-700 hover:bg-slate-100"
                href={item.href}
              >
                {item.label}
              </a>
            </li>
          {/each}
        </ul>
      </nav>
    </aside>

    <main class="flex-1 p-8">
      <slot />
    </main>
  </div>
</div>
