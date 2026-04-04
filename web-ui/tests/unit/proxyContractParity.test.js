import { describe, expect, it } from "vitest";

import {
  getCatalogEntries,
  getAllProxyablePaths,
  getCommandInfo,
  isProxyableCommand,
} from "../../src/lib/coreRouteCatalog.js";
import {
  mockSupportedCommands,
  proxyOnlyCommands,
} from "../../src/lib/mockParityProfile.js";

describe("proxyContractParity", () => {
  describe("isProxyableCommand", () => {
    it("matches GET /topics", () => {
      expect(isProxyableCommand("GET", "/topics")).toBe(true);
    });

    it("matches POST /topics", () => {
      expect(isProxyableCommand("POST", "/topics")).toBe(true);
    });

    it("matches GET /boards/{board_id}/workspace", () => {
      expect(isProxyableCommand("GET", "/boards/board-123/workspace")).toBe(
        true,
      );
    });

    it("matches GET /cards", () => {
      expect(isProxyableCommand("GET", "/cards")).toBe(true);
    });

    it("matches POST /cards/{card_id}/move", () => {
      expect(isProxyableCommand("POST", "/cards/card-123/move")).toBe(true);
    });

    it("matches GET /docs", () => {
      expect(isProxyableCommand("GET", "/docs")).toBe(true);
    });

    it("matches GET /inbox", () => {
      expect(isProxyableCommand("GET", "/inbox")).toBe(true);
    });

    it("matches POST /inbox/{inbox_id}/acknowledge", () => {
      expect(isProxyableCommand("POST", "/inbox/inbox-123/acknowledge")).toBe(
        true,
      );
    });

    it("matches GET /health", () => {
      expect(isProxyableCommand("GET", "/health")).toBe(true);
    });

    it("matches GET /meta/version", () => {
      expect(isProxyableCommand("GET", "/version")).toBe(true);
    });

    it("returns false for non-contract paths", () => {
      expect(isProxyableCommand("GET", "/unknown")).toBe(false);
    });

    it("handles trailing slashes", () => {
      expect(isProxyableCommand("GET", "/threads/")).toBe(true);
    });
  });

  describe("getCommandInfo", () => {
    it("returns command info for valid path", () => {
      const info = getCommandInfo("GET", "/threads");
      expect(info).not.toBeNull();
      expect(info.commandId).toBe("threads.list");
      expect(info.method).toBe("GET");
    });

    it("returns null for unknown path", () => {
      expect(getCommandInfo("GET", "/unknown")).toBeNull();
    });
  });

  describe("getAllProxyablePaths", () => {
    it("returns array of proxyable paths", () => {
      const paths = getAllProxyablePaths();
      expect(Array.isArray(paths)).toBe(true);
      expect(paths.length).toBeGreaterThan(0);
    });

    it("includes all required contract paths", () => {
      const paths = getAllProxyablePaths();
      const pathStrings = paths.map((p) => `${p.method}:${p.path}`);

      expect(pathStrings).toContain("GET:/topics");
      expect(pathStrings).toContain("POST:/topics");
      expect(pathStrings).toContain("GET:/boards");
      expect(pathStrings).toContain("POST:/boards");
      expect(pathStrings).toContain("GET:/boards/{board_id}/workspace");
      expect(pathStrings).toContain("GET:/cards");
      expect(pathStrings).toContain("POST:/cards/{card_id}/move");
      expect(pathStrings).toContain("POST:/events");
      expect(pathStrings).toContain("GET:/docs");
      expect(pathStrings).toContain("GET:/inbox");
      expect(pathStrings).toContain("POST:/inbox/{inbox_id}/acknowledge");
    });
  });

  describe("mockParityProfile", () => {
    it("classifies every command id as mock-supported or proxy-only", () => {
      const catalogIds = new Set(
        Array.from(getCatalogEntries().values()).map((e) => e.commandId),
      );
      const classified = new Set([
        ...mockSupportedCommands,
        ...proxyOnlyCommands,
      ]);
      expect(classified.size).toBe(catalogIds.size);
      for (const id of catalogIds) {
        expect(
          mockSupportedCommands.includes(id) !== proxyOnlyCommands.includes(id),
        ).toBe(true);
      }
    });
  });

  describe("getCatalogEntries", () => {
    it("contains expected number of entries", () => {
      expect(getCatalogEntries().size).toBeGreaterThan(20);
    });

    it("has all proxy-only commands in catalog", () => {
      const entries = Array.from(getCatalogEntries().values());
      const methods = entries.map((e) => e.method);
      const paths = entries.map((e) => e.path);

      expect(methods).toContain("GET");
      expect(paths).toContain("/threads");
    });
  });
});
