<script>
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";

  const textPreviewMaxChars = 256 * 1024;

  let { contentType = "", content = null, fileName = "" } = $props();

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
  <p class="text-meta text-[var(--fg-muted)]">No preview available.</p>
{:else if isImage && objectUrl}
  <div class="rounded-md border border-[var(--line)] bg-[var(--bg-soft)] p-2">
    <img
      src={objectUrl}
      alt={fileName || "Attachment"}
      class="max-h-[70vh] max-w-full object-contain"
    />
  </div>
{:else if isMarkdown && textPreview}
  <div class="rounded-md border border-[var(--line)] bg-[var(--bg-soft)] p-3">
    <MarkdownRenderer source={textPreview} />
  </div>
{:else if isTextPlain && textPreview}
  <pre
    class="max-h-[70vh] overflow-auto whitespace-pre-wrap rounded-md border border-[var(--line)] bg-[var(--bg-soft)] p-3 text-meta text-[var(--fg)]">{textPreview}</pre>
{:else if isJson}
  <div class="space-y-2">
    <div class="flex justify-end">
      {#if objectUrl}
        <a
          class="inline-flex items-center rounded-md border border-[var(--line)] bg-[var(--bg)] px-3 py-1.5 text-micro font-medium text-[var(--fg)] hover:bg-[var(--bg-soft)]"
          href={objectUrl}
          download={fileName || "attachment.json"}>Download</a
        >
      {/if}
    </div>
    <pre
      class="max-h-[70vh] overflow-auto whitespace-pre-wrap rounded-md border border-[var(--line)] bg-[var(--bg-soft)] p-3 text-meta text-[var(--fg)]">{jsonPreview.length >
      textPreviewMaxChars
        ? `${jsonPreview.slice(0, textPreviewMaxChars)}\n\n… (truncated)`
        : jsonPreview}</pre>
  </div>
{:else if isPdf}
  <div
    class="flex flex-wrap items-center gap-3 text-meta text-[var(--fg-muted)]"
  >
    <span>
      PDF attachment —
      <span class="text-[var(--fg)]">{fileName || "download"}</span>.
    </span>
    {#if objectUrl}
      <a
        class="inline-flex items-center rounded-md border border-[var(--line)] bg-[var(--bg)] px-3 py-1.5 text-micro font-medium text-[var(--fg)] hover:bg-[var(--bg-soft)]"
        href={objectUrl}
        download={fileName || "attachment.pdf"}>Download</a
      >
    {/if}
  </div>
{:else}
  <div
    class="flex flex-wrap items-center gap-3 text-meta text-[var(--fg-muted)]"
  >
    <span>
      Binary attachment ({displayContentType || "unknown"})
      {#if fileName}
        — {fileName}
      {/if}
      .
    </span>
    {#if objectUrl}
      <a
        class="inline-flex items-center rounded-md border border-[var(--line)] bg-[var(--bg)] px-3 py-1.5 text-micro font-medium text-[var(--fg)] hover:bg-[var(--bg-soft)]"
        href={objectUrl}
        download={fileName || "attachment"}>Download</a
      >
    {/if}
  </div>
{/if}
