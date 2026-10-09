import { reviseWorkspaceView } from "$lib/workspaceViewCache.js";

/** Reflect server-confirmed writes before navigation or a failed re-read. */
export function commitInboxView(
  scope,
  { answered, decision, archivedRef } = {},
) {
  reviseWorkspaceView(`${scope}:inbox`, (snapshot) => {
    if (!snapshot) return null;
    const completed = answered
      ? {
          ...snapshot[3]?.value?.items?.find((item) => item.id === answered.id),
          ...snapshot[4]?.value?.items?.find((item) => item.id === answered.id),
          ...answered,
        }
      : null;
    return snapshot.map((source, index) => {
      const key = index === 2 ? "work" : "items";
      if (!Array.isArray(source.value?.[key])) return source;
      let items = source.value[key];
      if (archivedRef)
        items = items.filter((item) =>
          index === 2
            ? item.ref !== archivedRef
            : item.work_ref !== archivedRef,
        );
      if (decision && index === 0)
        items = items.map((item) =>
          item.id === decision.id ? decision : item,
        );
      if (completed && index === 3)
        items = items.filter((item) => item.id !== completed.id);
      if (completed && index === 4)
        items = [
          ...items.filter((item) => item.id !== completed.id),
          completed,
        ];
      return { ...source, value: { ...source.value, [key]: items } };
    });
  });
}
