/** HttpOnly cookie set by POST /hosted/api/session (production builds). */
export const CP_ACCESS_TOKEN_COOKIE = "oar_cp_access_token";

/** Dev-only cookie readable from JS when running `vite dev`. */
export const CP_DEV_ACCESS_TOKEN_COOKIE = "oar_cp_dev_access_token";

export const CP_TOKEN_MAX_AGE_SEC = 60 * 60 * 24 * 30;
