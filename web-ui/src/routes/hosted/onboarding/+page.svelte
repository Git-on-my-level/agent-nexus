<script>
  import { onMount } from "svelte";
  import { goto } from "$app/navigation";

  onMount(() => {
    void goto("/hosted/dashboard", { replaceState: true });
  });
</script>

<svelte:head>
  <title>Redirecting…</title>
</svelte:head>

<p class="text-[12px] text-gray-500">Redirecting to your dashboard…</p>
