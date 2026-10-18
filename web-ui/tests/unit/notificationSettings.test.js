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
  rowEnabled,
  setRowEnabled,
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
        addressing: "settings",
        label: "",
        address: "a@example.com",
        push_enabled: true,
        digest_enabled: false,
      },
      {
        channel: "telegram",
        addressing: "settings",
        label: "",
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

  describe("principal-addressed channels", () => {
    const hosted = {
      channels: [],
      available_channels: ["email", "hosted"],
      channel_options: {
        email: { addressing: "settings", kinds: ["push", "digest"] },
        hosted: {
          addressing: "principal",
          kinds: ["push"],
          label: "Hosted notifications",
        },
      },
    };

    it("is on by default with no address and uses the declared label", () => {
      const row = draftFromSettings(hosted).rows[1];
      expect(row.addressing).toBe("principal");
      expect(rowEnabled(row)).toBe(true);
      expect(channelLabel(row.channel, row.label)).toBe("Hosted notifications");
      expect(settingsBody(draftFromSettings(hosted)).channels).toEqual([
        {
          channel: "hosted",
          address: "",
          push_enabled: true,
          digest_enabled: true,
        },
      ]);
      expect(draftDirty(draftFromSettings(hosted), hosted)).toBe(false);
    });

    it("keeps an off choice and never sends an address", () => {
      const draft = draftFromSettings(hosted);
      draft.rows[1].address = "should-not-send";
      setRowEnabled(draft.rows[1], false);
      expect(validateDraft(draft)).toEqual({});
      expect(settingsBody(draft).channels).toEqual([
        {
          channel: "hosted",
          address: "",
          push_enabled: false,
          digest_enabled: false,
        },
      ]);
      expect(draftDirty(draft, hosted)).toBe(true);
      expect(notificationPreview(draft)).toBe("Hosted notifications: off");
    });

    it("reads a saved off row back as off", () => {
      const saved = {
        ...hosted,
        channels: [
          {
            channel: "hosted",
            address: "",
            push_enabled: false,
            digest_enabled: false,
          },
        ],
      };
      expect(rowEnabled(draftFromSettings(saved).rows[1])).toBe(false);
      const restored = reconcileDraft(draftFromSettings(hosted), saved);
      expect(restored.rows[1].addressing).toBe("principal");
    });
  });
});
