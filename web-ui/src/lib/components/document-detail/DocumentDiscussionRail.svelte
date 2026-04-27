<script>
  import { browser } from "$app/environment";

  import DiscussionDrawer from "$lib/components/DiscussionDrawer.svelte";

  const LS_OPEN_KEY_LEGACY = "doc-discussion-rail-open";

  let {
    doc,
    workspaceSlug = "",
    workspaceId = "",
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
  } = $props();

  let threadId = $derived(String(doc?.thread_id ?? "").trim());
  let docId = $derived(String(doc?.id ?? "").trim());
  let documentRef = $derived(docId ? `document:${docId}` : "");

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

  // Empty-state copy that doubles as a discoverability hint: the operator
  // sees the floating "Comment" pill on the doc body only after they make a
  // selection, so a text-anchored mention here teaches the gesture before
  // they need it. Mod+Opt+M matches Google Docs' comment shortcut.
  const DOC_DISCUSSION_EMPTY =
    "No comments yet. Select text in the doc and press ⌘⌥M (Ctrl+Alt+M) to comment, or write a freeform note below.";
</script>

{#if threadId && docId}
  <DiscussionDrawer
    layout="rail"
    {threadId}
    postRouteScopeId={docId}
    {workspaceId}
    {workspaceSlug}
    label="Discussion"
    storageKey={`doc-discussion:${docId}`}
    emptyMessage={DOC_DISCUSSION_EMPTY}
    subjectRefFilter={documentRef}
    extraPostRefs={[documentRef]}
    archiveLabelKind="resolve"
    {pendingDocumentComment}
    {onPendingDocumentPostConsumed}
    {onClearPendingDocumentPost}
    {currentDocumentContent}
    {onDocumentTextAnchorContextChange}
    {openSignal}
    expandFillsParent
    narrowEdgeToEdge
  />
{/if}
