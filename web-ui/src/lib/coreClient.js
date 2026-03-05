import { getSelectedActorId } from "$lib/actorSession";
import { createOarCoreClient } from "$lib/oarCoreClient";

export const coreClient = createOarCoreClient({
  actorIdProvider: getSelectedActorId,
});
