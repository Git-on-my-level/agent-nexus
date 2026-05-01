function joinWithAnd(parts) {
  if (parts.length <= 1) {
    return parts[0] ?? "";
  }
  if (parts.length === 2) {
    return `${parts[0]} and ${parts[1]}`;
  }
  return `${parts.slice(0, -1).join(", ")}, and ${parts.at(-1)}`;
}

function shellArg(value) {
  const raw = String(value ?? "");
  if (/^[A-Za-z0-9_./:@%+=,-]+$/.test(raw)) {
    return raw;
  }
  return `'${raw.replaceAll("'", "'\\''")}'`;
}

export function buildRegistrationMessage(
  token,
  baseUrl,
  agentName = "",
  username = "",
) {
  const normalizedToken = String(token ?? "").trim();
  const normalizedBaseUrl =
    String(baseUrl ?? "").trim() || "<ANX_CORE_BASE_URL>";
  const normalizedAgentName = String(agentName ?? "").trim();
  const normalizedUsername = String(username ?? "").trim();
  const agentNameArg = normalizedAgentName
    ? shellArg(normalizedAgentName)
    : "'<agent-name>'";
  const usernameArg = normalizedUsername
    ? shellArg(normalizedUsername)
    : "'<username>'";

  const missingLabels = [];
  if (!normalizedAgentName) {
    missingLabels.push("agent profile name");
  }
  if (!normalizedUsername) {
    missingLabels.push("username");
  }

  const lines = [
    "Install the ANX CLI first if this machine does not already have it:",
    "",
    "  curl -sSfL https://raw.githubusercontent.com/Git-on-my-level/agent-nexus/main/scripts/install-anx.sh | sh",
    "",
    "Register with this ANX workspace using the invite token below.",
    "Use the anx-core API origin for --base-url (the same value as the workspace coreBaseUrl), not the web app path under /o/.../w/....",
    "",
  ];

  if (missingLabels.length > 0) {
    lines.push(
      `Replace the placeholder ${missingLabels.length === 1 ? "value" : "values"} for ${joinWithAnd(missingLabels)} before running the command.`,
      "The CLI requires --username; it will not choose one automatically. The --agent value is the local profile name stored on that machine.",
      "",
      "Run the following command:",
    );
  } else {
    lines.push("Run the following command:");
  }

  lines.push(
    "",
    `  anx --base-url ${normalizedBaseUrl} --agent ${agentNameArg} auth register --username ${usernameArg} --invite-token ${normalizedToken}`,
    "",
    "This invite token is single-use.",
  );

  return lines.join("\n");
}
