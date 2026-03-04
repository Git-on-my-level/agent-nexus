<script>
  import { onMount } from "svelte";
  import { page } from "$app/stores";

  import { actorRegistry, lookupActorDisplayName } from "$lib/actorSession";
  import ProvenanceBadge from "$lib/components/ProvenanceBadge.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import UnknownObjectPanel from "$lib/components/UnknownObjectPanel.svelte";
  import { coreClient } from "$lib/coreClient";
  import { toTimelineView } from "$lib/timelineUtils";

  $: threadId = $page.params.threadId;
  $: actorName = (actorId) => lookupActorDisplayName(actorId, $actorRegistry);

  let snapshot = null;
  let snapshotLoading = false;
  let snapshotError = "";

  let timeline = [];
  let timelineLoading = false;
  let timelineError = "";

  let messageText = "";
  let replyToEventId = "";
  let postingMessage = false;
  let postMessageError = "";

  onMount(async () => {
    await ensureActorRegistry();
    await loadThreadDetail(threadId);
  });

  $: timelineView = toTimelineView(timeline, { threadId });
  $: canPost = Boolean(messageText.trim()) && !postingMessage;

  async function ensureActorRegistry() {
    if ($actorRegistry.length > 0) {
      return;
    }

    try {
      const response = await coreClient.listActors();
      actorRegistry.set(response.actors ?? []);
    } catch {
      // Thread detail still renders with actor IDs if actor registry cannot be loaded.
    }
  }

  async function loadThreadDetail(targetThreadId) {
    await Promise.all([
      loadSnapshot(targetThreadId),
      loadTimeline(targetThreadId),
      ensureActorRegistry(),
    ]);
  }

  async function loadSnapshot(targetThreadId) {
    snapshotLoading = true;
    snapshotError = "";

    try {
      const response = await coreClient.getThread(targetThreadId);
      snapshot = response.thread ?? null;
    } catch (error) {
      const reason = error instanceof Error ? error.message : String(error);
      snapshotError = `Failed to load thread snapshot: ${reason}`;
      snapshot = null;
    } finally {
      snapshotLoading = false;
    }
  }

  async function loadTimeline(targetThreadId) {
    timelineLoading = true;
    timelineError = "";

    try {
      const response = await coreClient.listThreadTimeline(targetThreadId);
      timeline = response.events ?? [];
    } catch (error) {
      const reason = error instanceof Error ? error.message : String(error);
      timelineError = `Failed to load timeline: ${reason}`;
      timeline = [];
    } finally {
      timelineLoading = false;
    }
  }

  function setReplyTarget(eventId) {
    replyToEventId = eventId;
  }

  function clearReplyTarget() {
    replyToEventId = "";
  }

  async function postMessage() {
    if (!messageText.trim()) {
      postMessageError = "Message text is required.";
      return;
    }

    postingMessage = true;
    postMessageError = "";

    try {
      const refs = [`thread:${threadId}`];
      if (replyToEventId) {
        refs.push(`event:${replyToEventId}`);
      }

      await coreClient.createEvent({
        event: {
          type: "message_posted",
          thread_id: threadId,
          refs,
          summary: `Message: ${messageText.trim().slice(0, 100)}`,
          payload: {
            text: messageText.trim(),
          },
          provenance: {
            sources: ["actor_statement:ui"],
          },
        },
      });

      messageText = "";
      replyToEventId = "";
      await loadTimeline(threadId);
    } catch (error) {
      const reason = error instanceof Error ? error.message : String(error);
      postMessageError = `Failed to post message: ${reason}`;
    } finally {
      postingMessage = false;
    }
  }
</script>

<h1 class="text-2xl font-semibold">Thread Detail: {threadId}</h1>

{#if snapshotLoading}
  <p class="mt-4 rounded-md bg-white p-3 text-sm text-slate-700 shadow-sm">
    Loading thread snapshot...
  </p>
{:else if snapshotError}
  <p
    class="mt-4 rounded-md border border-rose-200 bg-rose-50 p-3 text-sm text-rose-800"
  >
    {snapshotError}
  </p>
{:else if !snapshot}
  <p class="mt-4 rounded-md bg-white p-3 text-sm text-slate-700 shadow-sm">
    Thread not found.
  </p>
{:else}
  <section
    class="mt-4 rounded-lg border border-slate-200 bg-white p-4 shadow-sm"
  >
    <h2 class="text-sm font-semibold uppercase tracking-wide text-slate-500">
      Snapshot
    </h2>

    <dl class="mt-3 grid gap-3 text-sm md:grid-cols-2">
      <div>
        <dt class="text-xs uppercase tracking-wide text-slate-500">title</dt>
        <dd class="font-medium text-slate-900">{snapshot.title}</dd>
      </div>
      <div>
        <dt class="text-xs uppercase tracking-wide text-slate-500">type</dt>
        <dd class="text-slate-800">{snapshot.type}</dd>
      </div>
      <div>
        <dt class="text-xs uppercase tracking-wide text-slate-500">status</dt>
        <dd class="text-slate-800">{snapshot.status}</dd>
      </div>
      <div>
        <dt class="text-xs uppercase tracking-wide text-slate-500">priority</dt>
        <dd class="text-slate-800">{snapshot.priority}</dd>
      </div>
      <div>
        <dt class="text-xs uppercase tracking-wide text-slate-500">cadence</dt>
        <dd class="text-slate-800">{snapshot.cadence}</dd>
      </div>
      <div>
        <dt class="text-xs uppercase tracking-wide text-slate-500">
          next check-in
        </dt>
        <dd class="text-slate-800">{snapshot.next_check_in_at || "none"}</dd>
      </div>
      <div>
        <dt class="text-xs uppercase tracking-wide text-slate-500">
          updated by
        </dt>
        <dd class="text-slate-800">{actorName(snapshot.updated_by)}</dd>
      </div>
      <div>
        <dt class="text-xs uppercase tracking-wide text-slate-500">
          updated at
        </dt>
        <dd class="text-slate-800">{snapshot.updated_at || "unknown"}</dd>
      </div>
    </dl>

    <div class="mt-3">
      <p class="text-xs uppercase tracking-wide text-slate-500">tags</p>
      <div class="mt-1 flex flex-wrap gap-2 text-xs">
        {#if (snapshot.tags ?? []).length === 0}
          <span class="text-slate-500">none</span>
        {:else}
          {#each snapshot.tags ?? [] as tag}
            <span class="rounded bg-slate-100 px-2 py-1">{tag}</span>
          {/each}
        {/if}
      </div>
    </div>

    <div class="mt-3">
      <p class="text-xs uppercase tracking-wide text-slate-500">
        current summary
      </p>
      <p class="mt-1 text-sm text-slate-800">{snapshot.current_summary}</p>
    </div>

    <div class="mt-3">
      <p class="text-xs uppercase tracking-wide text-slate-500">next actions</p>
      <ul class="mt-1 list-disc space-y-1 pl-5 text-sm text-slate-800">
        {#each snapshot.next_actions ?? [] as action}
          <li>{action}</li>
        {/each}
      </ul>
    </div>

    <div class="mt-3">
      <p class="text-xs uppercase tracking-wide text-slate-500">
        open commitments
      </p>
      <ul class="mt-1 list-disc space-y-1 pl-5 text-sm text-slate-800">
        {#if (snapshot.open_commitments ?? []).length === 0}
          <li>none</li>
        {:else}
          {#each snapshot.open_commitments ?? [] as commitmentId}
            <li id={`commitment-${commitmentId}`}>{commitmentId}</li>
          {/each}
        {/if}
      </ul>
    </div>

    <div class="mt-3">
      <ProvenanceBadge provenance={snapshot.provenance ?? { sources: [] }} />
    </div>

    <div class="mt-3">
      <UnknownObjectPanel
        objectData={snapshot}
        title="Raw Thread Snapshot JSON"
      />
    </div>
  </section>
{/if}

<section class="mt-6 rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
  <h2 class="text-sm font-semibold uppercase tracking-wide text-slate-500">
    Post Message
  </h2>

  {#if postMessageError}
    <p
      class="mt-2 rounded-md border border-rose-200 bg-rose-50 px-3 py-2 text-xs text-rose-800"
    >
      {postMessageError}
    </p>
  {/if}

  <label
    class="mt-3 block text-xs font-semibold uppercase tracking-wide text-slate-600"
    for="message-text"
  >
    Message
  </label>
  <textarea
    bind:value={messageText}
    class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
    id="message-text"
    rows="3"
  ></textarea>

  <label
    class="mt-3 block text-xs font-semibold uppercase tracking-wide text-slate-600"
    for="reply-target"
  >
    Reply to event (optional)
  </label>
  <select
    bind:value={replyToEventId}
    class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
    id="reply-target"
  >
    <option value="">No reply target</option>
    {#each timelineView as event}
      <option value={event.id}>{event.id} — {event.summary}</option>
    {/each}
  </select>

  <div class="mt-3 flex flex-wrap items-center gap-2">
    <button
      class="rounded-md bg-slate-900 px-3 py-1.5 text-xs font-semibold text-white hover:bg-slate-700 disabled:cursor-not-allowed disabled:opacity-60"
      disabled={!canPost}
      on:click={postMessage}
      type="button"
    >
      {postingMessage ? "Posting..." : "Post message"}
    </button>
    {#if replyToEventId}
      <p class="text-xs text-slate-600">
        Reply target: <span class="font-mono">{replyToEventId}</span>
      </p>
      <button
        class="rounded-md border border-slate-300 bg-white px-2 py-1 text-xs text-slate-700 hover:bg-slate-100"
        on:click={clearReplyTarget}
        type="button"
      >
        Clear
      </button>
    {/if}
  </div>
</section>

<section class="mt-6 space-y-3">
  <h2 class="text-sm font-semibold uppercase tracking-wide text-slate-500">
    Timeline
  </h2>

  {#if timelineLoading}
    <p class="rounded-md bg-white p-3 text-sm text-slate-700 shadow-sm">
      Loading timeline...
    </p>
  {:else if timelineError}
    <p
      class="rounded-md border border-rose-200 bg-rose-50 p-3 text-sm text-rose-800"
    >
      {timelineError}
    </p>
  {:else if timelineView.length === 0}
    <p class="rounded-md bg-white p-3 text-sm text-slate-700 shadow-sm">
      No timeline events for this thread yet.
    </p>
  {:else}
    {#each timelineView as event}
      <article
        class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm"
        id={`event-${event.id}`}
      >
        <p class="text-sm font-semibold text-slate-900">{event.summary}</p>
        <p class="mt-1 text-xs text-slate-600">
          type: {event.typeLabel}
          {#if !event.isKnownType}
            <span class="ml-1 font-mono text-slate-500"
              >({event.rawType || "unknown"})</span
            >
          {/if}
        </p>
        <p class="mt-1 text-xs text-slate-600">
          timestamp: {event.ts || "unknown"}
        </p>
        <p class="mt-1 text-xs text-slate-600">
          actor: {actorName(event.actor_id)}
        </p>

        {#if event.changedFields.length > 0}
          <div class="mt-2">
            <p
              class="text-xs font-semibold uppercase tracking-wide text-slate-500"
            >
              changed fields
            </p>
            <div class="mt-1 flex flex-wrap gap-2 text-xs">
              {#each event.changedFields as field}
                <span class="rounded bg-slate-100 px-2 py-1">{field}</span>
              {/each}
            </div>
          </div>
        {/if}

        <div class="mt-3 flex flex-wrap gap-2 text-xs">
          {#each event.refs as refValue}
            <span class="rounded bg-slate-100 px-2 py-1">
              <RefLink {refValue} {threadId} />
            </span>
          {/each}
        </div>

        <div class="mt-3">
          <ProvenanceBadge provenance={event.provenance ?? { sources: [] }} />
        </div>

        {#if !event.isKnownType}
          <div class="mt-2">
            <p
              class="text-xs font-semibold uppercase tracking-wide text-slate-500"
            >
              Unknown event details
            </p>
            <pre
              class="mt-1 overflow-auto rounded bg-slate-50 p-2 text-[11px] text-slate-700">{JSON.stringify(
                event.payload ?? {},
                null,
                2,
              )}</pre>
            <pre
              class="mt-1 overflow-auto rounded bg-slate-50 p-2 text-[11px] text-slate-700">{JSON.stringify(
                event.refs ?? [],
                null,
                2,
              )}</pre>
          </div>
        {/if}

        <div class="mt-3 flex flex-wrap gap-2">
          <button
            class="rounded-md border border-slate-300 bg-white px-2 py-1 text-xs text-slate-700 hover:bg-slate-100"
            on:click={() => setReplyTarget(event.id)}
            type="button"
          >
            Reply
          </button>
        </div>

        <div class="mt-3">
          <UnknownObjectPanel objectData={event} title="Raw Event JSON" />
        </div>
      </article>
    {/each}
  {/if}
</section>
