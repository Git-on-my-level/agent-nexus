<script>
  import ThreadDetailPage from "../../threads/[threadId]/+page.svelte";

  let { data } = $props();
</script>

<ThreadDetailPage {data} />
