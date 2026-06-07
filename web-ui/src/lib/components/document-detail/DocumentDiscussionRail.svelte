<script>
  import { browser } from "$app/environment";

  import DiscussionDrawer from "$lib/components/DiscussionDrawer.svelte";
  import { documentDiscussionSurface } from "$lib/discussionSurface";

  const LS_OPEN_KEY_LEGACY = "doc-discussion-rail-open";

  let {
    doc,
    workspaceSlug = "",
    workspaceId = "",
    /**
     * `discussion` — MessagesTab. `revisions` — document history (mutually exclusive).
     */
    docSidePanel = $bindable("discussion"),
    /** Body for the revisions tab; same chrome as discussion (DiscussionDrawer). */
    revisionPanel = undefined,
    /** Increment to force the rail open (e.g. when starting a text comment from the doc body). */
    openSignal = 0,
    /** Set when the operator is composing a document text comment anchored to a selection. */
    pendingDocumentComment = null,
    onPendingDocumentPostConsumed = undefined,
    onClearPendingDocumentPost = undefined,
    /**
     * Live snapshot of the document content currently displayed by the
     * parent doc page. Forwarded to MessagesTab so anchored comments whose
     * quoted text no longer occurs in the head render as "Text removed"
     * (read-side; no event mutation).
     */
    currentDocumentContent = "",
    onDocumentTextAnchorContextChange = undefined,
    /** When opening Revisions from the collapsed rail, fetch history if needed. */
    prepareRevisionHistory = undefined,
  } = $props();

  let surface = $derived(documentDiscussionSurface(doc));
  let threadId = $derived(surface.threadId);
  let docId = $derived(String(doc?.id ?? "").trim());

  /**
   * Migrate legacy per-doc / global open keys to `discussion-drawer:doc-discussion:{id}`
   * so desktop rail and mobile dock share one preference.
   */
  $effect.pre(() => {
    if (!browser || !docId) return;
    const nk = `discussion-drawer:doc-discussion:${docId}`;
    if (localStorage.getItem(nk) != null) return;
    const perDoc = localStorage.getItem(`doc-discussion-rail-open:${docId}`);
    if (perDoc === "1" || perDoc === "0") {
      localStorage.setItem(nk, perDoc);
      return;
    }
    const legacy = localStorage.getItem(LS_OPEN_KEY_LEGACY);
    if (legacy === "1" || legacy === "0") {
      localStorage.setItem(nk, legacy);
    }
  });

  let drawerSideTab = $derived(
    docSidePanel === "revisions" ? "secondary" : "messages",
  );
</script>

{#if threadId && docId}
  <DiscussionDrawer
    {...surface}
    postRouteScopeId={docId}
    {workspaceId}
    {workspaceSlug}
    {pendingDocumentComment}
    {onPendingDocumentPostConsumed}
    {onClearPendingDocumentPost}
    {currentDocumentContent}
    {onDocumentTextAnchorContextChange}
    {openSignal}
    secondaryPanel={revisionPanel}
    sideTab={drawerSideTab}
    onSideTabChange={(t) => {
      docSidePanel = t === "secondary" ? "revisions" : "discussion";
    }}
    prepareSecondaryPanel={prepareRevisionHistory}
  />
{/if}
