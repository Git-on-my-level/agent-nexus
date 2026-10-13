<script>
  import VisualReport from "$lib/components/reports/VisualReport.svelte";
  import { usePageTitle } from "$lib/pageTitle.svelte.js";

  let { data } = $props();

  usePageTitle(() => `${data.report.title} · Report preview`);
</script>

<main class="preview">
  <VisualReport report={data.report} previewObservations={data.observations} />
</main>

<style>
  .preview {
    max-width: 1440px;
    margin: 0 auto;
    padding: 32px;
  }
</style>
