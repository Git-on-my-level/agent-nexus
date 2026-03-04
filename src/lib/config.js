export const EXPECTED_SCHEMA_VERSION = "0.2.2";

export function normalizeBaseUrl(value) {
  if (!value) {
    return "";
  }

  return String(value).trim().replace(/\/+$/, "");
}

export const oarCoreBaseUrl = normalizeBaseUrl(
  import.meta.env.PUBLIC_OAR_CORE_BASE_URL,
);
