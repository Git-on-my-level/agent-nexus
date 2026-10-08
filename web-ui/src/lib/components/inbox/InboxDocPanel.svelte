<script>
  /**
   * A document named as ask evidence, read beside the question.
   *
   * Opening the doc route to check a reference meant losing the reply being
   * composed, so the evidence opens here instead: the same `docs.get` read the
   * document page makes, rendered through the same markdown surface, with a
   * link to the full page for everything this panel deliberately leaves out
   * (comments, revisions, editing).
   *
   * One request per document opened, cached for as long as the panel keeps it
   * open. Escape and the close button both dismiss it.
   */
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import Skeleton from "$lib/components/state/Skeleton.svelte";
  import { dismissOnEscape } from "$lib/actions/dismissOnEscape.js";
  import { coreClient } from "$lib/coreClient";
  import { splitTypedRef } from "$lib/inboxUtils.js";

  let {
    /** `document:<id>` to read, or "" for a closed panel. */
    ref = "",
    /** `(ref) => href` for the full document page. */
    hrefFor = () => "",
    onClose = null,
    organizationSlug = "",
    workspaceSlug = "",
    /**
     * `column`: a third column beside the item at lg, a sheet over the page
     * below it, where there is only one column to have. `sheet`: always a
     * sheet, for a surface that is one column at every width.
     */
    variant = "column",
  } = $props();

  const SHEET =
    "fixed inset-0 z-40 overflow-hidden border-t border-line bg-panel";
  let shellClass = $derived(
    variant === "sheet"
      ? `${SHEET} sm:inset-y-0 sm:left-auto sm:w-full sm:max-w-md sm:border-l sm:border-t-0`
      : `${SHEET} lg:static lg:z-auto lg:border-t-0 lg:border-l`,
  );

  let loading = $state(false);
  let error = $state("");
  let doc = $state(null);
  let revision = $state(null);
  let loadedRef = $state("");
  let sequence = 0;

  const TEXTUAL = new Set(["", "text", "markdown", "text/markdown"]);
  let contentType = $derived(String(revision?.content_type ?? "").trim());
  let readable = $derived(TEXTUAL.has(contentType));
  let title = $derived(
    String(doc?.title ?? "").trim() || splitTypedRef(ref).id,
  );

  $effect(() => {
    const target = ref;
    if (!target) {
      loadedRef = "";
      doc = null;
      revision = null;
      error = "";
      loading = false;
      return;
    }
    if (target === loadedRef) return;
    const id = splitTypedRef(target).id;
    if (!id) {
      error = "This evidence reference names no document.";
      return;
    }
    const ticket = ++sequence;
    loading = true;
    error = "";
    doc = null;
    revision = null;
    void coreClient.getDocument(id).then(
      (result) => {
        if (ticket !== sequence) return;
        doc = result?.document ?? null;
        revision = result?.revision ?? null;
        loadedRef = target;
        loading = false;
        if (!doc) error = "This document could not be read.";
      },
      () => {
        if (ticket !== sequence) return;
        loading = false;
        error = "This document could not be read.";
      },
    );
  });
</script>

{#if ref}
  <aside
    class="flex min-w-0 flex-col {shellClass}"
    aria-label={`Evidence document: ${title}`}
    data-inbox-doc-panel={ref}
    use:dismissOnEscape={{
      enabled: true,
      onDismiss: () => onClose?.(),
    }}
  >
    <div
      class="flex items-center gap-2 border-b border-line-subtle px-4 py-2 text-micro"
    >
      <span class="min-w-0 flex-1 truncate text-meta font-medium text-fg"
        >{title}</span
      >
      {#if hrefFor(ref)}
        <a
          class="shrink-0 text-fg-muted hover:text-fg"
          href={hrefFor(ref)}
          data-inbox-doc-panel-open>Open document</a
        >
      {/if}
      <button
        class="shrink-0 text-fg-subtle hover:text-fg"
        type="button"
        aria-label="Close evidence document"
        data-inbox-doc-panel-close
        onclick={() => onClose?.()}>×</button
      >
    </div>
    <div class="min-w-0 flex-1 overflow-auto px-4 py-3">
      {#if loading}
        <Skeleton rows={6} />
      {:else if error}
        <p class="text-meta text-fg-muted">{error}</p>
      {:else if !readable}
        <p class="text-meta text-fg-muted">
          This document is stored as {contentType}; open it to read it.
        </p>
      {:else}
        <MarkdownRenderer
          source={revision?.content ?? ""}
          class="text-meta leading-relaxed text-fg [overflow-wrap:anywhere]"
          {organizationSlug}
          {workspaceSlug}
        />
      {/if}
    </div>
  </aside>
{/if}
