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

function normalizeRegistrationArgs(
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
  const effectiveAgentName = normalizedAgentName || normalizedUsername;
  const agentNameArg = effectiveAgentName
    ? shellArg(effectiveAgentName)
    : "'<username>'";
  const usernameArg = normalizedUsername
    ? shellArg(normalizedUsername)
    : "'<username>'";

  return {
    normalizedToken,
    normalizedBaseUrl,
    normalizedUsername,
    agentNameArg,
    usernameArg,
  };
}

export function buildRegistrationCommand(
  token,
  baseUrl,
  agentName = "",
  username = "",
) {
  const { normalizedToken, normalizedBaseUrl, agentNameArg, usernameArg } =
    normalizeRegistrationArgs(token, baseUrl, agentName, username);

  return `anx --base-url ${normalizedBaseUrl} --agent ${agentNameArg} auth register --username ${usernameArg} --invite-token ${normalizedToken}`;
}

export function buildRegistrationMessage(
  token,
  baseUrl,
  agentName = "",
  username = "",
) {
  const { normalizedUsername } = normalizeRegistrationArgs(
    token,
    baseUrl,
    agentName,
    username,
  );

  const missingLabels = [];
  if (!normalizedUsername) {
    missingLabels.push("username");
  }

  const lines = [
    "Check whether the ANX CLI is already installed:",
    "",
    "  anx --version",
    "",
    "If that command is not found, install the ANX CLI:",
    "",
    "  curl -sSfL https://raw.githubusercontent.com/Git-on-my-level/agent-nexus/main/scripts/install-anx.sh | sh",
    "",
    "Register a new CLI profile with this ANX workspace using the invite token below.",
    "Use the anx-core API origin for --base-url (the same value as the workspace coreBaseUrl), not the web app path under /o/.../w/....",
    "",
  ];

  if (missingLabels.length > 0) {
    lines.push(
      `Replace the placeholder ${missingLabels.length === 1 ? "value" : "values"} for ${joinWithAnd(missingLabels)} before running the command.`,
      "The CLI requires --username; it will not choose one automatically. When --agent is not provided separately, use the same value as the workspace username.",
      "",
      "Run this registration command after filling in the placeholder:",
    );
  } else {
    lines.push("Run this registration command:");
  }

  lines.push(
    "",
    `  ${buildRegistrationCommand(token, baseUrl, agentName, username)}`,
    "",
    "This invite token is single-use.",
  );

  return lines.join("\n");
}
