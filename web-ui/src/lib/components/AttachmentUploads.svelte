<script>
  import { mutationOutbox } from "$lib/mutationOutbox.js";
  import MutationSendStatus from "$lib/components/MutationSendStatus.svelte";
  import { uploadedAttachments } from "$lib/attachmentWork.js";
  let { target } = $props();
  let pending = $derived(
    $mutationOutbox.filter(
      (entry) =>
        entry.operation === "artifacts.attachments.create" &&
        entry.request.composerTarget === target,
    ),
  );
</script>

{#each pending as entry (entry.key)}
  <MutationSendStatus {entry} />
{/each}
{#each $uploadedAttachments.filter((row) => row.target === target) as row (row.key)}
  <div
    class="mb-2 text-meta text-fg-muted"
    data-mutation-state="sent"
    role="status"
  >
    Uploaded · {row.artifact.original_filename || "Attachment"}
  </div>
{/each}
