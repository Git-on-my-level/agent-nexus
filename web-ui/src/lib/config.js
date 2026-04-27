export const EXPECTED_SCHEMA_VERSION = "0.4.0";

export function normalizeBaseUrl(value) {
  if (!value) {
    return "";
  }

  return String(value).trim().replace(/\/+$/, "");
}
