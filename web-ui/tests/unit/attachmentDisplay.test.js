import { describe, expect, it } from "vitest";

import {
  formatBytes,
  isTrashedAttachmentMeta,
  middleTruncateFilename,
  shortMimeBadge,
} from "../../src/lib/attachmentDisplay.js";

describe("attachmentDisplay", () => {
  describe("shortMimeBadge", () => {
    it("maps common MIME types", () => {
      expect(shortMimeBadge("image/png")).toBe("PNG");
      expect(shortMimeBadge("image/webp")).toBe("WEBP");
      expect(shortMimeBadge("application/pdf")).toBe("PDF");
      expect(shortMimeBadge("text/markdown")).toBe("MD");
      expect(shortMimeBadge("text/plain")).toBe("TXT");
      expect(shortMimeBadge("application/json")).toBe("JSON");
    });

    it("returns empty for missing type", () => {
      expect(shortMimeBadge("")).toBe("");
      expect(shortMimeBadge(null)).toBe("");
    });

    it("falls back to FILE for unknown structured MIME", () => {
      expect(shortMimeBadge("application/octet-stream")).toBe("FILE");
    });
  });

  describe("middleTruncateFilename", () => {
    it("does not shorten short names", () => {
      expect(middleTruncateFilename("a.png", 36)).toBe("a.png");
      expect(middleTruncateFilename("readme.md", 36)).toBe("readme.md");
    });

    it("preserves extension", () => {
      const long =
        "a-very-long-name-that-exceeds-limit-for-sure-final-version.png";
      const out = middleTruncateFilename(long, 36);
      expect(out.endsWith(".png")).toBe(true);
      expect(out).toContain("…");
      expect(out.length).toBeLessThanOrEqual(36);
    });

    it("handles no extension", () => {
      const stem = "x".repeat(50);
      const out = middleTruncateFilename(stem, 20);
      expect(out).toContain("…");
      expect(out.length).toBeLessThanOrEqual(20);
    });

    it("handles very short maxLen with extension", () => {
      const out = middleTruncateFilename("abcdefgh.pdf", 10);
      expect(out.endsWith(".pdf")).toBe(true);
      expect(out.length).toBeLessThanOrEqual(10);
    });
  });

  describe("formatBytes", () => {
    it("returns empty for zero or invalid", () => {
      expect(formatBytes(0)).toBe("");
      expect(formatBytes(-1)).toBe("");
      expect(formatBytes(Number.NaN)).toBe("");
    });

    it("formats boundary sizes", () => {
      expect(formatBytes(512)).toBe("512 B");
      expect(formatBytes(1023)).toBe("1023 B");
      expect(formatBytes(1024)).toBe("1 KB");
      expect(formatBytes(1536)).toBe("1.5 KB");
      expect(formatBytes(1024 * 1024)).toBe("1 MB");
      expect(formatBytes(Math.floor(1.5 * 1024 * 1024))).toBe("1.5 MB");
    });
  });

  describe("isTrashedAttachmentMeta", () => {
    it("is true when merged meta has trashed_at or trashedAt", () => {
      expect(
        isTrashedAttachmentMeta(
          { attachmentMeta: { trashed_at: "2026-01-01T00:00:00Z" } },
          null,
        ),
      ).toBe(true);
      expect(
        isTrashedAttachmentMeta(
          { attachmentMeta: {} },
          { trashedAt: "2026-01-01T00:00:00Z" },
        ),
      ).toBe(true);
    });

    it("is false when pending overlay has no trash field", () => {
      expect(isTrashedAttachmentMeta({ attachmentMeta: {} }, null)).toBe(false);
      expect(
        isTrashedAttachmentMeta(null, { original_filename: "x.png" }),
      ).toBe(false);
    });
  });
});
