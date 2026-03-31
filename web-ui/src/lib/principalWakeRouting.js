import { bridgeCheckinEventId, describeWakeRouting } from "$lib/wakeRouting";

/**
 * @param {object[]} principalList
 * @param {{ workspaceBindingTarget?: string, client: { getEvent: Function } }} options
 */
export async function enrichPrincipalsWithWakeRouting(
  principalList,
  { workspaceBindingTarget = "", client },
) {
  const activeAgentPrincipals = [
    ...new Set(
      (principalList ?? [])
        .filter(
          (principal) =>
            principal?.principal_kind === "agent" &&
            !principal?.revoked &&
            String(principal?.username ?? "").trim() !== "" &&
            principal?.registration,
        )
        .map((principal) => String(principal.username).trim()),
    ),
  ];

  const bridgeCheckins = new Map();

  await Promise.all(
    activeAgentPrincipals.map(async (handle) => {
      const principal = (principalList ?? []).find(
        (item) => String(item?.username ?? "").trim() === handle,
      );
      const checkinEventId = bridgeCheckinEventId(principal);
      if (!checkinEventId) {
        bridgeCheckins.set(handle, { state: "missing" });
        return;
      }
      try {
        bridgeCheckins.set(handle, {
          state: "ok",
          document: await client.getEvent(checkinEventId),
        });
      } catch (error) {
        bridgeCheckins.set(
          handle,
          error?.status === 404 ? { state: "missing" } : { state: "error" },
        );
      }
    }),
  );

  return Promise.all(
    (principalList ?? []).map(async (principal) => ({
      ...principal,
      wakeRouting: await describeWakeRouting(
        principal,
        null,
        workspaceBindingTarget,
        bridgeCheckins.get(String(principal?.username ?? "").trim()) ?? null,
      ),
    })),
  );
}
