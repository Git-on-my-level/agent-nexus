<script>
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";

  const textPreviewMaxChars = 256 * 1024;

  let {
    contentType = "",
    content = null,
    fileName = "",
    featured = false,
  } = $props();

  let objectUrl = $state("");

  let displayContentType = $derived(
    String(contentType ?? "")
      .split(";")[0]
      .trim(),
  );

  let textPreview = $derived.by(() => {
    if (typeof content !== "string") return "";
    if (content.length > textPreviewMaxChars) {
      return `${content.slice(0, textPreviewMaxChars)}\n\n… (truncated)`;
    }
    return content;
  });

  let jsonPreview = $derived.by(() => {
    if (displayContentType !== "application/json") return "";
    if (typeof content === "string") return content;
    try {
      return JSON.stringify(content, null, 2);
    } catch {
      return "";
    }
  });

  let isImage = $derived(
    displayContentType.startsWith("image/") && content instanceof ArrayBuffer,
  );
  let isJson = $derived(
    displayContentType === "application/json" && Boolean(jsonPreview),
  );
  let isTextPlain = $derived(
    displayContentType === "text/plain" && typeof content === "string",
  );
  let isMarkdown = $derived(
    (displayContentType === "text/markdown" ||
      displayContentType === "text/x-markdown") &&
      typeof content === "string",
  );
  let isPdf = $derived(
    displayContentType === "application/pdf" && content instanceof ArrayBuffer,
  );

  function contentLoadIssue(c) {
    return c === null || c === undefined;
  }

  $effect(() => {
    if (!(content instanceof ArrayBuffer) && !isJson) {
      if (objectUrl) {
        URL.revokeObjectURL(objectUrl);
        objectUrl = "";
      }
      return;
    }
    const blobContent = content instanceof ArrayBuffer ? content : jsonPreview;
    const blob = new Blob([blobContent], {
      type: displayContentType || "application/octet-stream",
    });
    const url = URL.createObjectURL(blob);
    objectUrl = url;
    return () => {
      URL.revokeObjectURL(url);
    };
  });
</script>

{#if contentLoadIssue(content)}
  <p class="text-meta text-fg-muted">No preview available.</p>
{:else if isImage && objectUrl}
  <div
    class="group relative overflow-hidden rounded-md border border-line bg-bg"
  >
    <a
      href={objectUrl}
      target="_blank"
      rel="noopener"
      class="block"
      title="Open full size in new tab"
    >
      <img
        src={objectUrl}
        alt={fileName || "Attachment"}
        class="mx-auto block max-w-full object-contain {featured
          ? 'max-h-[80vh] w-full'
          : 'max-h-[70vh]'}"
      />
    </a>
    <a
      class="absolute right-2 top-2 hidden rounded-md border border-line bg-bg px-2 py-1 text-micro font-medium text-fg opacity-0 backdrop-blur transition-opacity hover:bg-bg-soft group-hover:opacity-100 sm:inline-flex"
      href={objectUrl}
      download={fileName || "attachment"}>Download</a
    >
  </div>
{:else if isMarkdown && textPreview}
  <div class="rounded-md border border-line bg-bg-soft p-3">
    <MarkdownRenderer source={textPreview} />
  </div>
{:else if isTextPlain && textPreview}
  <pre
    class="max-h-[70vh] overflow-auto whitespace-pre-wrap rounded-md border border-line bg-bg-soft p-3 text-meta text-fg">{textPreview}</pre>
{:else if isJson}
  <div class="space-y-2">
    <div class="flex justify-end">
      {#if objectUrl}
        <a
          class="inline-flex items-center rounded-md border border-line bg-bg px-3 py-1.5 text-micro font-medium text-fg hover:bg-bg-soft"
          href={objectUrl}
          download={fileName || "attachment.json"}>Download</a
        >
      {/if}
    </div>
    <pre
      class="max-h-[70vh] overflow-auto whitespace-pre-wrap rounded-md border border-line bg-bg-soft p-3 text-meta text-fg">{jsonPreview.length >
      textPreviewMaxChars
        ? `${jsonPreview.slice(0, textPreviewMaxChars)}\n\n… (truncated)`
        : jsonPreview}</pre>
  </div>
{:else if isPdf}
  {#if featured && objectUrl}
    <div class="overflow-hidden rounded-md border border-line bg-bg">
      <object
        data={objectUrl}
        type="application/pdf"
        class="block h-[80vh] w-full"
        aria-label={fileName || "PDF attachment"}
      >
        <div
          class="flex flex-wrap items-center gap-3 p-4 text-meta text-fg-muted"
        >
          <span>PDF preview unavailable.</span>
          <a
            class="inline-flex items-center rounded-md border border-line bg-bg px-3 py-1.5 text-micro font-medium text-fg hover:bg-bg-soft"
            href={objectUrl}
            download={fileName || "attachment.pdf"}>Download</a
          >
        </div>
      </object>
    </div>
  {:else}
    <div class="flex flex-wrap items-center gap-3 text-meta text-fg-muted">
      <span>
        PDF attachment —
        <span class="text-fg">{fileName || "download"}</span>.
      </span>
      {#if objectUrl}
        <a
          class="inline-flex items-center rounded-md border border-line bg-bg px-3 py-1.5 text-micro font-medium text-fg hover:bg-bg-soft"
          href={objectUrl}
          download={fileName || "attachment.pdf"}>Download</a
        >
      {/if}
    </div>
  {/if}
{:else}
  <div class="flex flex-wrap items-center gap-3 text-meta text-fg-muted">
    <span>
      Binary attachment ({displayContentType || "unknown"})
      {#if fileName}
        — {fileName}
      {/if}
      .
    </span>
    {#if objectUrl}
      <a
        class="inline-flex items-center rounded-md border border-line bg-bg px-3 py-1.5 text-micro font-medium text-fg hover:bg-bg-soft"
        href={objectUrl}
        download={fileName || "attachment"}>Download</a
      >
    {/if}
  </div>
{/if}
