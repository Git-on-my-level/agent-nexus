<script>
  import { browser } from "$app/environment";
  import {
    formatBytes,
    isTrashedAttachmentMeta,
    middleTruncateFilename,
    shortMimeBadge,
  } from "$lib/attachmentDisplay.js";
  import RefChip from "$lib/components/RefChip.svelte";
  import { coreClient } from "$lib/coreClient";

  let {
    /** Resolved ref from resolveRefLink */
    resolved,
    /** Merged over timeline meta (composer upload response, etc.) */
    artifactOverlay = null,
    pending = false,
    uploadProgress = null,
    /** @type {'inline' | 'compact' | 'block'} */
    size = "inline",
    /** Extra classes on outer shell */
    class: clazz = "",
  } = $props();

  let downloadBusy = $state(false);

  let mergedMeta = $derived.by(() => {
    const base =
      resolved?.attachmentMeta && typeof resolved.attachmentMeta === "object"
        ? resolved.attachmentMeta
        : {};
    const over =
      artifactOverlay && typeof artifactOverlay === "object"
        ? artifactOverlay
        : {};
    return { ...base, ...over };
  });

  let artifactId = $derived(String(resolved?.value ?? "").trim());

  let state = $derived.by(() => {
    if (pending) return "pending";
    // `routed` means the id was in `artifactRoutesById` (timeline had metadata).
    // Unrouted `artifact:` refs still get a direct `/artifacts/:id` href from
    // `resolveRefLink`; only treat as missing when that href could not be built.
    if (
      !resolved?.routed &&
      resolved?.prefix === "artifact" &&
      !resolved?.isLink
    ) {
      return "missing";
    }
    return "ready";
  });

  /** Trashed attachments are omitted from message/topic attachment surfaces; restore clears `trashed_at` and the chip shows again. */
  let suppressRender = $derived(
    !pending && isTrashedAttachmentMeta(resolved, artifactOverlay),
  );

  let displayName = $derived.by(() => {
    const m = mergedMeta;
    const fn = String(m.original_filename ?? m.originalFilename ?? "").trim();
    if (fn) return fn;
    const pl = String(resolved?.primaryLabel ?? "").trim();
    const sep = pl.indexOf(" · ");
    if (sep > 0) return pl.slice(0, sep).trim();
    return pl || artifactId;
  });

  let typeBadge = $derived.by(() => {
    const m = mergedMeta;
    const ct = String(m.content_type ?? m.contentType ?? "").trim();
    if (ct) return shortMimeBadge(ct);
    const pl = String(resolved?.primaryLabel ?? "").trim();
    const sep = pl.indexOf(" · ");
    if (sep > 0) return pl.slice(sep + 3).trim();
    return "";
  });

  let truncatedName = $derived.by(() => {
    const max = size === "compact" ? 28 : size === "block" ? 42 : 36;
    return middleTruncateFilename(displayName, max);
  });

  let sizeLine = $derived.by(() => {
    if (size === "compact") return "";
    const m = mergedMeta;
    const b = m.size_bytes ?? m.sizeBytes;
    return formatBytes(Number(b));
  });

  let ariaLabelText = $derived.by(() => {
    if (pending) {
      return `Uploading ${displayName}`;
    }
    if (state === "missing") {
      return `Attachment unavailable: ${artifactId}`;
    }
    const parts = [
      `Attachment: ${displayName}`,
      typeBadge || "",
      sizeLine || "",
    ].filter(Boolean);
    return parts.join(", ");
  });

  let leadingScale = $derived(
    size === "block"
      ? "h-14 w-14 shrink-0 rounded-md bg-[var(--bg-soft)]"
      : "h-4 w-4 shrink-0",
  );

  async function handleDownload(ev) {
    ev.preventDefault();
    ev.stopPropagation();
    if (
      !browser ||
      !artifactId ||
      downloadBusy ||
      pending ||
      state === "missing"
    ) {
      return;
    }
    downloadBusy = true;
    try {
      const res = await coreClient.getArtifactContent(artifactId);
      const ct = String(res.contentType ?? "application/octet-stream");
      const body = res.content;
      let blob;
      if (body instanceof ArrayBuffer) {
        blob = new Blob([body], { type: ct });
      } else if (typeof body === "string") {
        blob = new Blob([body], { type: ct });
      } else {
        blob = new Blob([JSON.stringify(body)], { type: ct });
      }
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      const m = mergedMeta;
      a.download =
        String(
          m.original_filename ?? m.originalFilename ?? displayName,
        ).trim() || `artifact-${artifactId}`;
      a.click();
      URL.revokeObjectURL(url);
    } finally {
      downloadBusy = false;
    }
  }

  function compactArtifactId(id) {
    const s = String(id ?? "").trim();
    if (
      /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(s)
    ) {
      return s.slice(0, 10);
    }
    return s.length > 12 ? s.slice(0, 10) : s;
  }
</script>

{#if !suppressRender}
  {#snippet fileIcon()}
    <svg
      class="text-[var(--fg-muted)]"
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="2"
      aria-hidden="true"
    >
      <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
      <polyline points="14 2 14 8 20 8" />
    </svg>
  {/snippet}

  {#snippet spinner()}
    <span
      class="inline-block h-3.5 w-3.5 animate-spin rounded-full border-2 border-[var(--line-strong)] border-t-accent-solid"
      aria-hidden="true"
    ></span>
  {/snippet}

  <span
    class="attachment-chip-root compact-ref-link inline-flex min-w-0 max-w-full flex-col gap-0.5 {clazz}"
  >
    {#if pending}
      <RefChip
        navigable={false}
        accentText={false}
        ariaLabel={ariaLabelText}
        ariaBusy="true"
        role="text"
        title={resolved?.raw ?? ""}
        class="items-center gap-1.5 text-[var(--fg-muted)]"
      >
        <span class="{leadingScale} flex items-center justify-center"
          >{@render spinner()}</span
        >
        <span class="min-w-0 flex-1">
          <span class="block truncate font-medium text-[var(--fg)]"
            >Uploading… {truncatedName}</span
          >
          {#if uploadProgress && Number.isFinite(uploadProgress.loaded) && Number.isFinite(uploadProgress.total) && uploadProgress.total > 0}
            <span class="text-micro text-[var(--fg-muted)]">
              {formatBytes(uploadProgress.loaded)} / {formatBytes(
                uploadProgress.total,
              )}
            </span>
          {/if}
        </span>
      </RefChip>
    {:else}
      <RefChip
        navigable={false}
        accentText={false}
        title={resolved?.raw ?? ""}
        class="attachment-chip-row items-stretch gap-0 p-0 focus-within:ring-2 focus-within:ring-accent-solid/40 {state ===
        'missing'
          ? 'opacity-75'
          : ''}"
      >
        {#if resolved?.isLink && resolved?.href}
          <a
            class="compact-ref-link attachment-chip-link inline-flex min-w-0 flex-1 items-center gap-1.5 px-1.5 py-0.5 hover:border-[var(--line-strong)] {state ===
            'missing'
              ? 'text-[var(--fg-muted)]'
              : 'text-accent-text hover:text-accent-text'}"
            href={resolved.href}
            rel={resolved.isExternal ? "noreferrer noopener" : undefined}
            target={resolved.isExternal ? "_blank" : undefined}
            aria-label={ariaLabelText}
          >
            <span class="{leadingScale} flex items-center justify-center"
              >{@render fileIcon()}</span
            >
            <span
              class="flex min-w-0 flex-1 flex-col gap-0 sm:flex-row sm:items-baseline sm:gap-1.5"
            >
              {#if state === "missing"}
                <span class="min-w-0 truncate font-medium">
                  Artifact {compactArtifactId(artifactId)} — unavailable
                </span>
              {:else}
                <span class="min-w-0 truncate font-medium">{truncatedName}</span
                >
                {#if size !== "compact"}
                  <span
                    class="shrink-0 text-micro font-medium uppercase tracking-wide text-[var(--fg-muted)]"
                  >
                    {#if typeBadge}{typeBadge}{/if}{#if typeBadge && sizeLine}
                      {" · "}{/if}{#if sizeLine}{sizeLine}{/if}
                  </span>
                {:else if typeBadge}
                  <span
                    class="shrink-0 text-micro font-medium uppercase tracking-wide text-[var(--fg-muted)]"
                    >{typeBadge}</span
                  >
                {/if}
              {/if}
            </span>
          </a>
        {:else}
          <span
            class="compact-ref-link inline-flex min-w-0 flex-1 items-center gap-1.5 px-1.5 py-0.5 text-[var(--fg-muted)]"
            aria-label={ariaLabelText}
          >
            <span class="{leadingScale} flex items-center justify-center"
              >{@render fileIcon()}</span
            >
            <span class="min-w-0 truncate">
              {#if state === "missing"}
                Artifact {compactArtifactId(artifactId)} — unavailable
              {:else}
                {truncatedName}
              {/if}
            </span>
          </span>
        {/if}

        {#if browser && resolved?.isLink && resolved?.href && state === "ready" && !resolved?.isExternal}
          <button
            type="button"
            class="hidden shrink-0 items-center justify-center border-l border-[var(--line)] px-2 text-accent-text hover:bg-[var(--bg-soft)] sm:flex"
            aria-label={`Download ${displayName}`}
            disabled={downloadBusy}
            onclick={handleDownload}
          >
            <svg
              width="14"
              height="14"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              aria-hidden="true"
            >
              <path
                d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4M7 10l5 5 5-5M12 15V3"
              />
            </svg>
          </button>
        {/if}
      </RefChip>
    {/if}
  </span>
{/if}
