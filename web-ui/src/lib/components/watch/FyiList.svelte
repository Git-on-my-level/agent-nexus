<script>
  /**
   * "For your information": what agents told the reader without wanting an
   * answer. A digest that arrives every day is one row that says how many
   * times it arrived. Nothing here can be answered; a choice belongs in
   * Decide.
   */
  import { formatAgo } from "$lib/time/format.js";
  import { fyiRepeatText } from "$lib/briefOutcome.js";

  let {
    /** `brief.fyi`. */
    items = [],
    now = Date.now(),
    /** `(ref) => href` for the subject card. */
    hrefFor,
  } = $props();
</script>

{#if items.length}
  <section
    class="mt-5"
    aria-labelledby="watch-fyi-heading"
    data-source="computed:fyi"
    data-watch-fyi
  >
    <h2 id="watch-fyi-heading" class="head" data-ui-label>
      For your information · {items.length}
    </h2>
    <ul class="m-0 list-none p-0">
      {#each items as item (item.ref)}
        <li class="fyi" data-fyi-row={item.ref}>
          <p class="m-0 font-medium text-fg [overflow-wrap:anywhere]">
            {item.title}
          </p>
          {#if item.text && item.text !== item.title}
            <p
              class="m-0 mt-0.5 text-meta text-fg-muted [overflow-wrap:anywhere]"
            >
              {item.text}
            </p>
          {/if}
          <p class="m-0 mt-0.5 text-meta text-fg-muted">
            {#if item.author}<span data-author-chip>{item.author}</span><span
                aria-hidden="true"
                class="text-fg-subtle">{" · "}</span
              >{/if}<span>{formatAgo(item.at, now)}</span
            >{#if item.subject}<span aria-hidden="true" class="text-fg-subtle"
                >{" · "}</span
              ><a
                class="text-accent-text hover:underline"
                href={hrefFor?.(item.subject.ref)}
                >{item.subject.title || item.subject.ref}</a
              >{/if}{#if item.repeats > 0}<span
                aria-hidden="true"
                class="text-fg-subtle">{" · "}</span
              ><span data-fyi-repeats>{fyiRepeatText(item.repeats)}</span>{/if}
          </p>
        </li>
      {/each}
    </ul>
  </section>
{/if}

<style>
  .head {
    margin: 0;
    padding: 6px 0;
    border-bottom: 1px solid var(--line);
    color: var(--fg-muted);
    font-size: 13px;
    font-weight: 600;
    letter-spacing: 0.04em;
    line-height: 18px;
    text-transform: uppercase;
  }
  .fyi {
    border-bottom: 1px solid var(--line-subtle);
    padding: 10px 4px;
  }
</style>
