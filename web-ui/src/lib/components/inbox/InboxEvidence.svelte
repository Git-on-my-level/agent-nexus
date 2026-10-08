<script>
  /**
   * The evidence under an ask's question.
   *
   * The authoring standard asks for Decision, Context, what each option does,
   * and Evidence — and an ask that named a document without linking it left
   * the reader to go and find it. This renders what the ask already backed its
   * question with: native resources as typed refs, and external work as the
   * labelled links the asking agent wrote.
   *
   * A document opens in the Inbox's own side panel (`onOpenDoc`), so reading
   * the evidence does not cost the reader the answer they were composing.
   * Anything else links to its own page.
   *
   * External links show what kind of thing they are when the URL says so
   * ("Pull request", "Commit") and their host. They do not show a title, state
   * or checks: core holds nothing about a third-party URL, and fetching
   * github.com from the operator's browser is neither cheap nor this
   * surface's to do. The link, its label and its kind are what is available.
   */
  let {
    evidence = null,
    /** `(ref) => href` for a native ref, from the page. */
    hrefFor = () => "",
    /** `(ref) => title` from data the page already loaded; falls back to the id. */
    labelFor = () => "",
    /** Opens a document in the side panel instead of navigating. */
    onOpenDoc = null,
    openDocRef = "",
  } = $props();

  let refs = $derived(evidence?.refs ?? []);
  let links = $derived(evidence?.links ?? []);
  function canPanel(entry) {
    return entry.prefix === "document" && typeof onOpenDoc === "function";
  }
  function label(entry) {
    return String(labelFor(entry.ref) ?? "").trim() || entry.id;
  }
</script>

{#if refs.length || links.length}
  <div data-inbox-evidence>
    <p class="ui-label">Evidence</p>
    <ul class="space-y-1">
      {#each refs as entry (entry.ref)}
        {@const href = hrefFor(entry.ref)}
        <li class="flex min-w-0 items-baseline gap-2 text-meta">
          <span class="shrink-0 text-micro text-fg-subtle">{entry.noun}</span>
          {#if canPanel(entry)}
            <button
              class="min-w-0 flex-1 truncate text-left text-fg hover:underline"
              type="button"
              aria-pressed={openDocRef === entry.ref ? "true" : "false"}
              data-inbox-evidence-doc={entry.ref}
              onclick={() => onOpenDoc(entry.ref)}>{label(entry)}</button
            >
          {:else if href}
            <a
              class="min-w-0 flex-1 truncate text-fg hover:underline"
              {href}
              data-inbox-evidence-ref={entry.ref}>{label(entry)}</a
            >
          {:else}
            <span
              class="min-w-0 flex-1 truncate text-fg-muted"
              data-inbox-evidence-ref={entry.ref}>{entry.ref}</span
            >
          {/if}
        </li>
      {/each}
      {#each links as link (link.url)}
        <li class="flex min-w-0 items-baseline gap-2 text-meta">
          <span class="shrink-0 text-micro text-fg-subtle"
            >{link.kind || "Link"}</span
          >
          <a
            class="min-w-0 flex-1 truncate text-fg hover:underline"
            href={link.url}
            target="_blank"
            rel="noreferrer"
            data-inbox-evidence-link={link.url}
            >{link.label}{#if link.host}<span class="text-fg-subtle">
                · {link.host}</span
              >{/if} ↗</a
          >
        </li>
      {/each}
    </ul>
    {#if evidence?.overrideReason}
      <!-- The author knowingly shipped an ask the authoring lint objected to,
           and said why. That belongs next to the evidence it is about. -->
      <p class="mt-1.5 text-micro text-fg-muted [overflow-wrap:anywhere]">
        Authoring override: {evidence.overrideReason}
      </p>
    {/if}
  </div>
{/if}
