import { describe, expect, it } from "vitest";

import {
  isProxyableCommand,
  getCommandInfo,
  getAllProxyablePaths,
  catalogByPath,
} from "../../src/lib/coreRouteCatalog.js";

describe("proxyContractParity", () => {
  describe("isProxyableCommand", () => {
    it("matches GET /threads", () => {
      expect(isProxyableCommand("GET", "/threads")).toBe(true);
    });

    it("matches GET /threads/{id}", () => {
      expect(isProxyableCommand("GET", "/threads/thread-123")).toBe(true);
    });

    it("matches POST /threads", () => {
      expect(isProxyableCommand("POST", "/threads")).toBe(true);
    });

    it("matches GET /commitments", () => {
      expect(isProxyableCommand("GET", "/commitments")).toBe(true);
    });

    it("matches GET /artifacts", () => {
      expect(isProxyableCommand("GET", "/artifacts")).toBe(true);
    });

    it("matches GET /inbox", () => {
      expect(isProxyableCommand("GET", "/inbox")).toBe(true);
    });

    it("matches POST /inbox/ack", () => {
      expect(isProxyableCommand("POST", "/inbox/ack")).toBe(true);
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

      expect(pathStrings).toContain("GET:/threads");
      expect(pathStrings).toContain("POST:/threads");
      expect(pathStrings).toContain("GET:/commitments");
      expect(pathStrings).toContain("GET:/artifacts");
      expect(pathStrings).toContain("GET:/inbox");
      expect(pathStrings).toContain("POST:/inbox/ack");
    });
  });

  describe("catalogByPath", () => {
    it("contains expected number of entries", () => {
      expect(catalogByPath.size).toBeGreaterThan(20);
    });

    it("has all proxy-only commands in catalog", () => {
      const entries = Array.from(catalogByPath.values());
      const methods = entries.map((e) => e.method);
      const paths = entries.map((e) => e.path);

      expect(methods).toContain("GET");
      expect(paths).toContain("/threads");
    });
  });
});
