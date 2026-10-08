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
  import { readerScope } from "$lib/readerScope.js";

  let {
    /** `document:<id>` to read, or "" for a closed panel. */
    ref = "",
    /** `(ref) => href` for the full document page. */
    hrefFor = () => "",
    onClose = null,
    /**
     * `(ref, title)` once the title is read, so the evidence row can use it.
     * Called only for a read that is still the current one, for the reader who
     * made it — a title is a read, and belongs to that reader alone.
     */
    onTitle = null,
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

  /*
   * The panel is a reading column at lg and a sheet over the page below it, so
   * it is not a modal and does not trap Tab — but it does appear on a click, so
   * focus moves into it and goes back to whatever opened it when it closes.
   * Without that, dismissing the sheet on a phone stranded focus on `<body>`.
   */
  let shell = $state(null);
  let opener = null;
  $effect(() => {
    const node = shell;
    if (!node) return;
    opener = globalThis.document?.activeElement ?? null;
    node.focus?.({ preventScroll: true });
    return () => {
      const target = opener;
      opener = null;
      if (target?.isConnected) target.focus?.({ preventScroll: true });
    };
  });

  let loading = $state(false);
  let error = $state("");
  let doc = $state(null);
  let revision = $state(null);
  /*
   * What is on screen and what is on its way, as plain variables: this is the
   * effect's own bookkeeping, and making it reactive would feed back into the
   * effect that writes it. Both are keyed by reader *and* ref, because the
   * answer to "do we already have this document?" is different for a different
   * reader — core authorizes every read.
   */
  let loadedKey = null;
  let inFlightKey = null;
  let sequence = 0;

  const TEXTUAL = new Set(["", "text", "markdown", "text/markdown"]);
  let contentType = $derived(String(revision?.content_type ?? "").trim());
  let readable = $derived(TEXTUAL.has(contentType));
  let title = $derived(
    String(doc?.title ?? "").trim() || splitTypedRef(ref).id,
  );

  $effect(() => {
    const target = ref;
    const scope = $readerScope;
    const key = `${scope}|${target}`;
    // Already on screen, or already on its way, for this reader: nothing to do,
    // and nothing in flight to cancel.
    if (key === loadedKey || key === inFlightKey) return;
    /*
     * Everything else invalidates what is in flight — the panel closed, the
     * reader opened another document, or the acting reader changed. A result
     * that lands after any of those must not be shown, must not be remembered
     * as loaded, and must not be handed back as a title: a read authorized for
     * one reader is not the next reader's to see, and a `loadedKey` left set
     * for it would serve that content again with no fresh read.
     */
    sequence += 1;
    const ticket = sequence;
    loadedKey = null;
    inFlightKey = null;
    doc = null;
    revision = null;
    error = "";
    loading = false;
    if (!target) return;
    const id = splitTypedRef(target).id;
    if (!id) {
      error = "This evidence reference names no document.";
      return;
    }
    inFlightKey = key;
    loading = true;
    const current = () => ticket === sequence && scope === $readerScope;
    void coreClient.getDocument(id).then(
      (result) => {
        if (!current()) return;
        inFlightKey = null;
        doc = result?.document ?? null;
        revision = result?.revision ?? null;
        loadedKey = key;
        loading = false;
        if (!doc) error = "This document could not be read.";
        const read = String(doc?.title ?? "").trim();
        if (read) onTitle?.(target, read);
      },
      () => {
        if (!current()) return;
        inFlightKey = null;
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
    tabindex="-1"
    bind:this={shell}
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
