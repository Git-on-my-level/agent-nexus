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
  import Time from "$lib/time/Time.svelte";
  import { sinceYouLastLookedStrip } from "$lib/sinceYouLastLooked.js";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import { page } from "$app/stores";

  let { digest = null, limit = 10, now = Date.now() } = $props();

  let strip = $derived(sinceYouLastLookedStrip(digest, { limit }));
  /** A baseline exists, so "nothing new" is an answer rather than a guess. */
  let baseline = $derived(String(digest?.since ?? "").trim());
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
      <!--
        The baseline is the previous *visit*, which can be months back. `<Time>`
        is the one formatter for that: it reads "3 h ago", "yesterday" or a
        short local date, so a quiet Friday cannot read as this morning, and
        the exact local instant is a hover away.
      -->
      <span class="text-micro text-fg-subtle"
        >last looked <Time value={baseline} {now} verb="looked" /></span
      >
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
              {#if item.label}<span class="changes-label">{item.label}</span
                >{/if}
            </li>
          {/each}
          {#if strip.overflow || strip.truncated}
            <li class="changes-more">
              {strip.overflow ? `+${strip.overflow} more` : "more not shown"}
            </li>
          {/if}
        </ul>
      {:else}
        <p class="text-meta text-fg-muted" data-overview-changes-empty>
          Nothing new.
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
