<script>
  /**
   * What changed in this workspace since the viewer last opened the Overview.
   *
   * One of the two sections the Overview exists for, alongside the initiative
   * cards. It was a thin strip under the morning brief; the brief is folded
   * away now, so this is a section in its own right.
   *
   * The digest arrives with the Overview snapshot, so it costs no request.
   * A first visit renders nothing — there is no baseline to compare against,
   * and "nothing changed" would be a claim rather than an answer. Once there
   * is a baseline and nothing has moved, it says so in one line, because on a
   * primary section a silent disappearance is indistinguishable from a bug.
   */
  import { sinceYouLastLookedStrip } from "$lib/sinceYouLastLooked.js";
  import { briefClock } from "$lib/morningBrief.js";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import { page } from "$app/stores";

  let { digest = null, limit = 10 } = $props();

  let strip = $derived(sinceYouLastLookedStrip(digest, { limit }));
  /** A baseline exists, so "nothing new" is an answer rather than a guess. */
  let baseline = $derived(String(digest?.since ?? "").trim());
  let clock = $derived(briefClock(baseline));
  let workspaceHref = $derived(
    bindWorkspaceHref($page?.params?.organization, $page?.params?.workspace),
  );
  const href = (ref) =>
    ref.startsWith("card:")
      ? workspaceHref(`/tasks/${encodeURIComponent(ref)}`)
      : "";
</script>

{#if baseline}
  <section
    class="rounded-md border border-line bg-panel"
    aria-labelledby="overview-changes"
    data-overview-section="changes"
    data-since-you-last-looked
  >
    <header
      class="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1 border-b border-line px-3 py-2"
    >
      <h2 id="overview-changes" class="text-subtitle text-fg">
        Recent changes
      </h2>
      {#if strip?.summary}<span class="text-meta text-fg-muted"
          >{strip.summary}</span
        >{/if}
      {#if clock}<span class="text-micro text-fg-subtle">since {clock}</span
        >{/if}
    </header>
    <div class="p-3">
      {#if strip}
        <ul class="changes">
          {#each strip.items as item (`${item.kind}:${item.ref}:${item.stepId}`)}
            <li data-since-kind={item.kind}>
              <span class="changes-dot" data-tone={item.tone} aria-hidden="true"
              ></span>
              {#if href(item.ref)}
                <a href={href(item.ref)}>{item.title}</a>
              {:else}
                <span>{item.title}</span>
              {/if}
              <span class="changes-label">{item.label}</span>
            </li>
          {/each}
          {#if strip.overflow || strip.truncated}
            <li class="changes-more">
              {strip.overflow ? `+${strip.overflow} more` : "more not shown"}
            </li>
          {/if}
        </ul>
      {:else}
        <p class="text-meta text-fg-muted" data-overview-empty>
          {clock ? `Nothing new since ${clock}.` : "Nothing new."}
        </p>
      {/if}
    </div>
  </section>
{/if}

<style>
  .changes {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 14px;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .changes li {
    display: flex;
    align-items: baseline;
    gap: 6px;
    min-width: 0;
    font-size: 11px;
    color: var(--fg-muted);
  }
  .changes a {
    color: var(--accent-text);
    overflow-wrap: anywhere;
  }
  .changes a:hover {
    text-decoration: underline;
  }
  .changes-dot {
    flex: none;
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--line-strong);
    transform: translateY(-1px);
  }
  .changes-dot[data-tone="ok"] {
    background: var(--ok-text, var(--accent-solid));
  }
  .changes-dot[data-tone="warn"] {
    background: var(--warn-text);
  }
  .changes-dot[data-tone="danger"] {
    background: var(--danger-text, var(--warn-text));
  }
  .changes-label {
    flex: none;
    color: var(--fg-subtle, var(--fg-muted));
  }
  .changes-more {
    color: var(--fg-subtle, var(--fg-muted));
  }
</style>
