<script>
  /**
   * The one filter pattern for workspace lists (Tasks, Docs, Threads).
   *
   * A disclosure rather than a header button that opens a panel: the header's
   * action row is for the page's primary verb, and three sibling lists had
   * grown three differently shaped ways to reach the same kind of control.
   *
   * Live rather than draft-then-Apply. Every control inside narrows the list
   * as it is set, so the reader sees what the thing they just clicked did
   * instead of clicking Apply to find out — and because the pages keep their
   * filters in the URL, what Apply used to commit is now shareable and
   * restored by Back.
   *
   * `openWhenFiltered` is for arriving on a filtered URL: the summary says
   * how many filters are on, and the controls that explain the short list are
   * already open.
   */
  import { untrack } from "svelte";

  let {
    activeCount = 0,
    toggleTestId = undefined,
    panelTestId = undefined,
    openWhenFiltered = false,
    onclear = undefined,
    children,
  } = $props();

  /*
   * Seeded once, then the reader owns it. Re-reading the prop would slam the
   * disclosure shut the moment a filter cleared itself.
   */
  let open = $state(untrack(() => openWhenFiltered));
</script>

<details class="text-micro text-fg-muted" bind:open data-ui="list-filters">
  <!-- One expression, not a text node and a block: the markup form rendered
       "Filters· 2 active", because the whitespace before the separator sat on
       the line the template was wrapped onto. -->
  <summary class="w-fit cursor-pointer" data-testid={toggleTestId}>
    {activeCount ? `Filters · ${activeCount} active` : "Filters"}
  </summary>
  <!-- Each control keeps its own label beside it: as loose siblings they wrap
       apart on a phone, leaving a label sitting next to the previous field. -->
  <div
    class="mt-2 flex flex-wrap items-end gap-x-3 gap-y-2"
    data-testid={panelTestId}
  >
    {@render children()}
    {#if activeCount && onclear}
      <button class="ui-prose-link" type="button" onclick={onclear}
        >Clear filters</button
      >
    {/if}
  </div>
</details>
