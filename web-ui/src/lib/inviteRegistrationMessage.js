function joinWithAnd(parts) {
  if (parts.length <= 1) {
    return parts[0] ?? "";
  }
  if (parts.length === 2) {
    return `${parts[0]} and ${parts[1]}`;
  }
  return `${parts.slice(0, -1).join(", ")}, and ${parts.at(-1)}`;
}

export function buildRegistrationMessage(
  token,
  baseUrl,
  agentName = "",
  username = "",
) {
  const normalizedToken = String(token ?? "").trim();
  const normalizedBaseUrl =
    String(baseUrl ?? "").trim() || "<OAR_WORKSPACE_URL>";
  const normalizedAgentName = String(agentName ?? "").trim();
  const normalizedUsername = String(username ?? "").trim();

  const missingLabels = [];
  if (!normalizedAgentName) {
    missingLabels.push("agent profile name");
  }
  if (!normalizedUsername) {
    missingLabels.push("username");
  }

  const lines = [
    "Register with this OAR workspace using the invite token below.",
    "",
  ];

  if (missingLabels.length > 0) {
    lines.push(
      `If you want to set your own ${joinWithAnd(missingLabels)}, replace the placeholder values before running the command.`,
      `If you leave the placeholders in place, OAR chooses ${joinWithAnd(missingLabels.map((label) => `the ${label}`))} during registration.`,
      "",
      "Run the following command:",
    );
  } else {
    lines.push("Run the following command:");
  }

  lines.push(
    "",
    `  oar --base-url ${normalizedBaseUrl} --agent ${normalizedAgentName || "<agent-name>"} auth register --username ${normalizedUsername || "<username>"} --invite-token ${normalizedToken}`,
    "",
    "This invite token is single-use.",
  );

  return lines.join("\n");
}
