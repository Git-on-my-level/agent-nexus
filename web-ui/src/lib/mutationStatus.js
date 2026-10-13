/** Never put an upstream response body in ordinary user-facing copy. */
export function mutationFailure(error, subject = "response") {
  if (error?.localPersistence || error?.publicMessage) {
    return {
      state: "attention",
      message: error.publicMessage || error.message,
      detail: error.publicMessage ? error.message : "",
      status: 400,
    };
  }
  const status = Number(error?.status || 0);
  const retryable =
    !status || status >= 500 || status === 408 || status === 429;
  const messages = {
    400: "This response needs a correction before it can be sent.",
    401: "Sign in again to send this response.",
    403: "You do not have permission to send this response.",
    404: "This item is no longer available.",
    409: "This item changed or has already been answered. Review your response.",
    422: "This response needs a correction before it can be sent.",
  };
  if (subject === "file") {
    Object.assign(messages, {
      400: "This file needs a correction before it can be uploaded.",
      401: "Sign in again to upload this file.",
      403: "You do not have permission to upload this file.",
      409: "This upload conflicts with an earlier request. Review the saved file.",
      413: "This file is too large. Choose a smaller file.",
      422: "This file needs a correction before it can be uploaded.",
    });
  }
  return {
    state: retryable ? "retrying" : "attention",
    message: retryable
      ? subject === "file"
        ? "The service could not confirm the upload. Your file is kept on this device."
        : "The service could not confirm your response. Your text is kept on this device."
      : messages[status] ||
        "This response needs your attention. Your text is kept on this device.",
    // A bounded diagnostic, rendered only behind a disclosure as plain text.
    detail: String(error?.message || error || "Unknown send error").slice(
      0,
      2000,
    ),
    status,
  };
}
