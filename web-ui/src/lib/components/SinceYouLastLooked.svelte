<script>
  /**
   * A compact strip of what changed since this viewer last opened the Overview.
   *
   * The digest arrives with the Overview snapshot, so this costs no request.
   * It renders nothing on a first visit or a quiet one — an empty strip saying
   * "no changes" is noise on a dashboard whose point is density.
   */
  import { sinceYouLastLookedStrip } from "$lib/sinceYouLastLooked.js";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import { page } from "$app/stores";

  let { digest = null } = $props();

  let strip = $derived(sinceYouLastLookedStrip(digest));
  let workspaceHref = $derived(
    bindWorkspaceHref($page?.params?.organization, $page?.params?.workspace),
  );
  const href = (ref) =>
    ref.startsWith("card:")
      ? workspaceHref(`/tasks/${encodeURIComponent(ref)}`)
      : "";
</script>

{#if strip}
  <section
    class="since"
    aria-label="Since you last looked"
    data-since-you-last-looked
  >
    <p class="since-head">
      <span class="since-title">Since you last looked</span>
      {#if strip.summary}<span class="since-summary">{strip.summary}</span>{/if}
    </p>
    <ul class="since-items">
      {#each strip.items as item (`${item.kind}:${item.ref}:${item.stepId}`)}
        <li data-since-kind={item.kind}>
          <span class="since-dot" data-tone={item.tone} aria-hidden="true"
          ></span>
          {#if href(item.ref)}
            <a href={href(item.ref)}>{item.title}</a>
          {:else}
            <span>{item.title}</span>
          {/if}
          <span class="since-label">{item.label}</span>
        </li>
      {/each}
      {#if strip.overflow || strip.truncated}
        <li class="since-more">
          {strip.overflow ? `+${strip.overflow} more` : "more not shown"}
        </li>
      {/if}
    </ul>
  </section>
{/if}

<style>
  .since {
    padding: 9px 12px;
    border: 1px solid var(--line);
    border-radius: 6px;
    background: var(--panel);
  }
  .since-head {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 4px 10px;
  }
  .since-title {
    font-size: 12px;
    font-weight: 600;
    color: var(--fg);
  }
  .since-summary {
    color: var(--fg-muted);
    font-size: 11px;
  }
  .since-items {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 14px;
    margin: 6px 0 0;
    padding: 0;
    list-style: none;
  }
  .since-items li {
    display: flex;
    align-items: baseline;
    gap: 6px;
    min-width: 0;
    font-size: 11px;
    color: var(--fg-muted);
  }
  .since-items a {
    color: var(--accent-text);
    overflow-wrap: anywhere;
  }
  .since-items a:hover {
    text-decoration: underline;
  }
  .since-dot {
    flex: none;
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--line-strong);
    transform: translateY(-1px);
  }
  .since-dot[data-tone="ok"] {
    background: var(--ok-text, var(--accent-solid));
  }
  .since-dot[data-tone="warn"] {
    background: var(--warn-text);
  }
  .since-dot[data-tone="danger"] {
    background: var(--danger-text, var(--warn-text));
  }
  .since-label {
    flex: none;
    color: var(--fg-subtle, var(--fg-muted));
  }
  .since-more {
    color: var(--fg-subtle, var(--fg-muted));
  }
</style>
