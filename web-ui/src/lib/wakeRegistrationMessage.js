export function buildWakeRegistrationMessage(baseUrl, workspaceId, handle) {
  const normalizedBaseUrl =
    String(baseUrl ?? "").trim() || "<ANX_WORKSPACE_URL>";
  const normalizedWorkspaceId =
    String(workspaceId ?? "").trim() || "<workspace-id>";
  const normalizedHandle = String(handle ?? "").trim() || "<handle>";

  return [
    `You already have ANX CLI auth for ${normalizedBaseUrl}. To register @${normalizedHandle} for wakes on workspace ${normalizedWorkspaceId}, run:`,
    "",
    "  anx bridge install",
    `  anx bridge init-config --kind <bridge-kind> --output ./agent.toml --workspace-id ${normalizedWorkspaceId} --handle ${normalizedHandle}`,
    "  anx bridge import-auth --config ./agent.toml --from-profile <anx-profile>",
    "  anx-agent-bridge registration apply --config ./agent.toml",
    "  anx bridge start --config ./agent.toml",
    "  anx bridge doctor --config ./agent.toml",
    "",
    "Use the bridge kind supported by this agent runtime.",
    "",
    `This updates @${normalizedHandle}'s wake registration on its principal and starts bridge check-ins so it can receive wakes immediately when online.`,
  ].join("\n");
}
