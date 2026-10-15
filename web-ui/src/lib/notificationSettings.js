import { enqueueLocalMutation } from "$lib/mutationOutbox.js";

/** One device-only draft per principal and workspace; the local work scope
 * already carries both, so the target names only the resource. */
export const NOTIFICATION_SETTINGS_TARGET = "notifications:settings";
export const NOTIFICATION_SETTINGS_OPERATION = "notifications.settings.set";

const LABELS = { email: "Email", telegram: "Telegram" };
const ADDRESS_LABELS = { email: "Email address", telegram: "Telegram chat ID" };

export const channelLabel = (channel) =>
  LABELS[channel] ||
  String(channel || "")
    .replace(/[-_]+/g, " ")
    .replace(/^./, (c) => c.toUpperCase());
export const addressLabel = (channel) =>
  ADDRESS_LABELS[channel] || `${channelLabel(channel)} address`;

const emptyRow = (channel) => ({
  channel,
  address: "",
  push_enabled: true,
  digest_enabled: true,
});

/** One row per channel an adapter can deliver. Saved values win over defaults. */
export function draftFromSettings(settings) {
  const saved = new Map(
    (settings?.channels ?? []).map((row) => [row.channel, row]),
  );
  return {
    rows: (settings?.available_channels ?? []).map((channel) => {
      const row = saved.get(channel);
      return row
        ? {
            channel,
            address: String(row.address ?? ""),
            push_enabled: Boolean(row.push_enabled),
            digest_enabled: Boolean(row.digest_enabled),
          }
        : emptyRow(channel);
    }),
  };
}

/** Apply a restored device draft over the latest canonical channel list. Rows
 * for channels that are no longer available are dropped, new ones appear. */
export function reconcileDraft(draft, settings) {
  const base = draftFromSettings(settings);
  const restored = new Map(
    (draft?.rows ?? []).map((row) => [row.channel, row]),
  );
  return {
    rows: base.rows.map((row) => {
      const mine = restored.get(row.channel);
      return mine
        ? {
            channel: row.channel,
            address: String(mine.address ?? ""),
            push_enabled: Boolean(mine.push_enabled),
            digest_enabled: Boolean(mine.digest_enabled),
          }
        : row;
    }),
  };
}

const normalized = (row) => ({
  channel: row.channel,
  address: String(row.address ?? "").trim(),
  push_enabled: Boolean(row.push_enabled),
  digest_enabled: Boolean(row.digest_enabled),
});

/** Rows with an address, in the shape core accepts. A blank address removes
 * the channel because the request replaces the caller's whole list. */
export function settingsBody(draft) {
  return {
    channels: (draft?.rows ?? [])
      .map(normalized)
      .filter((row) => row.address !== ""),
  };
}

/** Returns a plain-language problem per channel, or an empty object. */
export function validateDraft(draft) {
  const problems = {};
  for (const row of (draft?.rows ?? []).map(normalized)) {
    if (row.address === "") continue;
    if (
      row.channel === "email" &&
      !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(row.address)
    )
      problems[row.channel] = "Enter a valid email address.";
    else if (row.address.length > 320)
      problems[row.channel] = "This address is too long.";
  }
  return problems;
}

export function draftDirty(draft, settings) {
  return (
    JSON.stringify(settingsBody(draft).channels) !==
    JSON.stringify(
      settingsBody(draftFromSettings(settings)).channels.map((row) => row),
    )
  );
}

export function notificationPreview(draft) {
  const { channels } = settingsBody(draft);
  if (!channels.length) return "Turn off all notification channels";
  return channels
    .map(
      (row) =>
        `${channelLabel(row.channel)}: ${row.address}\n  Immediate alerts: ${row.push_enabled ? "on" : "off"}\n  Daily digest: ${row.digest_enabled ? "on" : "off"}`,
    )
    .join("\n");
}

/** Persist the complete request before any network send. The PUT replaces the
 * whole list, so replaying the frozen body is safe. */
export function enqueueNotificationSettings(draft, options = {}) {
  const problems = validateDraft(draft);
  if (Object.keys(problems).length) throw new Error(Object.values(problems)[0]);
  const frozen = JSON.parse(JSON.stringify(draft));
  return enqueueLocalMutation({
    ...options,
    key: crypto.randomUUID(),
    operation: NOTIFICATION_SETTINGS_OPERATION,
    request: { body: settingsBody(frozen) },
    target: NOTIFICATION_SETTINGS_TARGET,
    draft: frozen,
    title: "Saved notification settings",
    preview: notificationPreview(frozen),
  });
}
