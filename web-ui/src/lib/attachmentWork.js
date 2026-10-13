import { get, writable } from "svelte/store";
import { localWorkScope, canUseLocalWorkScope } from "$lib/localWorkScope.js";
import {
  listLocalWork,
  saveLocalWork,
  localWorkEpoch,
  localWorkPrincipal,
} from "$lib/localWorkStore.js";

export const uploadedAttachments = writable([]);
if (typeof window !== "undefined")
  window.addEventListener("anx:local-work-purged", (event) => {
    if (event.detail?.principal === localWorkPrincipal(get(localWorkScope)))
      uploadedAttachments.set([]);
  });
export async function refreshUploadedAttachments(scope) {
  if (!canUseLocalWorkScope(scope)) {
    uploadedAttachments.set([]);
    return;
  }
  const epoch = localWorkEpoch(scope);
  const rows = await listLocalWork(scope, "attachment");
  if (
    scope === get(localWorkScope) &&
    epoch === localWorkEpoch(scope) &&
    canUseLocalWorkScope(scope)
  )
    uploadedAttachments.set(rows.map((row) => row.value));
}
export async function saveUploadedAttachment(entry, result) {
  await saveLocalWork(
    entry.scope,
    "attachment",
    entry.key,
    {
      key: entry.key,
      target: entry.request.composerTarget,
      artifact: result.artifact,
    },
    { epoch: entry.epoch },
  );
}
export async function removeUploadedAttachment(ref) {
  const scope = get(localWorkScope);
  for (const row of get(uploadedAttachments))
    if (`artifact:${row.artifact.id}` === ref)
      await saveLocalWork(scope, "attachment", row.key, null);
  await refreshUploadedAttachments(scope);
}
export async function consumeUploadedAttachments(
  scope,
  refs,
  epoch = localWorkEpoch(scope),
) {
  const rows = await listLocalWork(scope, "attachment");
  const accepted = new Set(refs || []);
  // Canonical send confirmation remains true even when local receipt cleanup
  // fails. The store retains failed deletions in memory and warns on close.
  await Promise.allSettled(
    rows
      .filter((row) => accepted.has(`artifact:${row.value.artifact.id}`))
      .map((row) =>
        saveLocalWork(scope, "attachment", row.target, null, { epoch }),
      ),
  );
  await refreshUploadedAttachments(scope);
}

export function attachmentFile(request) {
  const raw = atob(request.file.bytes);
  const bytes = Uint8Array.from(raw, (char) => char.charCodeAt(0));
  return new File([bytes], request.file.name, { type: request.file.type });
}
