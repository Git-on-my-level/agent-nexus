<script>
  import { onMount } from "svelte";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import { workspacePath } from "$lib/workspacePaths";

  // The shell mounts this page only after activation and identity checks.
  // Navigate once without reloading or mounting Overview's data loaders twice.
  onMount(() => {
    void goto(
      workspacePath(
        $page.params.organization,
        $page.params.workspace,
        `/overview${$page.url.search}${$page.url.hash}`,
      ),
      { replaceState: true },
    );
  });
</script>

<p class="py-10 text-center text-meta text-fg-muted">Opening Overview…</p>
