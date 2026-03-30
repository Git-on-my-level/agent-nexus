import { registrationDocumentId } from "$lib/wakeRouting.js";

export function buildWakeRegistrationMessage(baseUrl, workspaceId, handle) {
  const normalizedBaseUrl =
    String(baseUrl ?? "").trim() || "<OAR_WORKSPACE_URL>";
  const normalizedWorkspaceId =
    String(workspaceId ?? "").trim() || "<workspace-id>";
  const normalizedHandle = String(handle ?? "").trim() || "<handle>";

  return [
    `You already have OAR CLI auth for ${normalizedBaseUrl}. To register @${normalizedHandle} for wakes on workspace ${normalizedWorkspaceId}, run:`,
    "",
    "  oar bridge install",
    `  oar bridge init-config --kind <bridge-kind> --output ./agent.toml --workspace-id ${normalizedWorkspaceId} --handle ${normalizedHandle}`,
    "  oar bridge import-auth --config ./agent.toml --from-profile <your-oar-profile>",
    "  oar-agent-bridge registration apply --config ./agent.toml",
    "  oar bridge start --config ./agent.toml",
    "  oar bridge doctor --config ./agent.toml",
    "",
    "Use the bridge kind your agent runtime supports.",
    "",
    `This writes ${registrationDocumentId(normalizedHandle)} and starts bridge check-ins so @${normalizedHandle} can receive wakes immediately when online.`,
  ].join("\n");
}
