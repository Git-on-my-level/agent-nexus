/**
 * Telling "you may not do this" apart from "your session is gone".
 *
 * Administration reads and writes answer 403 / `auth_admin_required` for a
 * principal that holds no administration. That is an answer the surface can
 * state plainly. A 401 / `auth_required` is a different thing entirely: the
 * session expired or was revoked, and the reader may well be an administrator.
 * Showing the refusal copy for a 401 tells an administrator they are not one,
 * so only 403 counts here and a 401 stays on the error path.
 */

function errorCode(error) {
  return String(error?.body?.error?.code ?? "");
}

/**
 * @param {unknown} error
 * @returns {boolean} true when the principal lacks administration authority
 */
export function isAdministrationRefusal(error) {
  if (Number(error?.status) === 403) return true;
  return errorCode(error) === "auth_admin_required";
}
