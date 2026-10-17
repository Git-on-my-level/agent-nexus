<script>
  import { onMount } from "svelte";

  let { children } = $props();
  let content;
  let empty = $state(false);

  onMount(() => {
    // Render errors have a boundary, but a route snippet may complete without
    // producing any DOM. Check after Svelte flushes and again on tab return.
    // Only inspect direct nodes; never walk the page or poll the workspace.
    const check = () => {
      if (document.hidden) return;
      empty =
        content.children.length === 0 &&
        !Array.from(content.childNodes).some(
          (node) => node.nodeType === Node.TEXT_NODE && node.textContent.trim(),
        );
    };
    const observer = new MutationObserver(check);
    observer.observe(content, { childList: true, characterData: true });
    document.addEventListener("visibilitychange", check);
    check();
    return () => {
      observer.disconnect();
      document.removeEventListener("visibilitychange", check);
    };
  });
</script>

<div bind:this={content} class="contents" data-workspace-route-content>
  {@render children?.()}
</div>
{#if empty}
  <div class="p-6" role="alert" data-workspace-empty-error>
    <p>This page could not be displayed.</p>
    <button
      class="btn-secondary mt-3"
      type="button"
      onclick={() => window.location.reload()}>Retry</button
    >
  </div>
{/if}
