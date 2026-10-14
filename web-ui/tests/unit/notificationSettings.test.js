import { describe, expect, it, vi } from "vitest";

vi.mock("$lib/mutationOutbox.js", () => ({ enqueueLocalMutation: vi.fn() }));

const {
  draftDirty,
  draftFromSettings,
  reconcileDraft,
  settingsBody,
  validateDraft,
  notificationPreview,
  channelLabel,
} = await import("../../src/lib/notificationSettings.js");

const settings = {
  channels: [
    {
      channel: "email",
      address: "a@example.com",
      push_enabled: true,
      digest_enabled: false,
      updated_at: "2026-01-01T00:00:00Z",
    },
  ],
  available_channels: ["email", "telegram"],
};

describe("notification settings helpers", () => {
  it("builds one row per available channel with defaults for unset ones", () => {
    expect(draftFromSettings(settings).rows).toEqual([
      {
        channel: "email",
        address: "a@example.com",
        push_enabled: true,
        digest_enabled: false,
      },
      {
        channel: "telegram",
        address: "",
        push_enabled: true,
        digest_enabled: true,
      },
    ]);
    expect(
      draftFromSettings({ channels: [], available_channels: [] }).rows,
    ).toEqual([]);
  });

  it("sends only rows with an address and trims it", () => {
    const draft = draftFromSettings(settings);
    draft.rows[1].address = "  12345 ";
    expect(settingsBody(draft).channels.map((r) => r.address)).toEqual([
      "a@example.com",
      "12345",
    ]);
    draft.rows[0].address = "";
    expect(settingsBody(draft).channels).toHaveLength(1);
  });

  it("validates email addresses but leaves blank rows alone", () => {
    const draft = draftFromSettings(settings);
    expect(validateDraft(draft)).toEqual({});
    draft.rows[0].address = "nope";
    expect(validateDraft(draft).email).toMatch(/valid email/);
    draft.rows[0].address = "";
    expect(validateDraft(draft)).toEqual({});
  });

  it("detects changes against the saved list", () => {
    const draft = draftFromSettings(settings);
    expect(draftDirty(draft, settings)).toBe(false);
    draft.rows[0].digest_enabled = true;
    expect(draftDirty(draft, settings)).toBe(true);
  });

  it("keeps restored edits and follows the current channel list", () => {
    const restored = {
      rows: [
        {
          channel: "email",
          address: "new@example.com",
          push_enabled: false,
          digest_enabled: false,
        },
        {
          channel: "sms",
          address: "1",
          push_enabled: true,
          digest_enabled: true,
        },
      ],
    };
    const merged = reconcileDraft(restored, settings);
    expect(merged.rows.map((r) => r.channel)).toEqual(["email", "telegram"]);
    expect(merged.rows[0].address).toBe("new@example.com");
    expect(merged.rows[1].address).toBe("");
  });

  it("labels channels and previews the request", () => {
    expect(channelLabel("telegram")).toBe("Telegram");
    expect(channelLabel("matrix_chat")).toBe("Matrix chat");
    expect(notificationPreview(draftFromSettings(settings))).toContain(
      "Daily digest: off",
    );
  });
});
