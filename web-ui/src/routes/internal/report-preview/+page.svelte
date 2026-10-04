<script>
  import VisualReport from "$lib/components/reports/VisualReport.svelte";

  let { data } = $props();
</script>

<svelte:head>
  <title>{data.report.title} · Report preview</title>
</svelte:head>

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
