/** Never put an upstream response body in ordinary user-facing copy. */
export function mutationFailure(error, subject = "response") {
  if (error?.localPersistence || error?.publicMessage) {
    return {
      state: "attention",
      message: error.publicMessage || error.message,
      detail: error.publicMessage ? error.message : "",
      status: 400,
      ...(error.replayExpired ? { expired: true } : {}),
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
  if (subject === "inbox action") {
    Object.assign(messages, {
      400: "Review the saved inbox request before sending it.",
      401: "Sign in again to send this inbox request.",
      403: "You do not have permission to make this inbox change.",
      409: "This inbox item changed. Your request is saved. Use latest version to review it.",
      422: "Review the saved inbox request before sending it.",
    });
  }
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
  if (subject === "move") {
    Object.assign(messages, {
      400: "This move needs a correction. Your destination and evidence are saved.",
      401: "Sign in again to send this move.",
      403: "You do not have permission to move this initiative.",
      409: "This initiative changed. Review the saved move, then use the latest version.",
      422: "Check the destination and evidence before sending this move.",
    });
  }
  if (subject === "notification settings") {
    Object.assign(messages, {
      400: "Check the saved notification settings. A channel may no longer be installed.",
      401: "Sign in again to save notification settings.",
      403: "Only people can change notification settings.",
      409: "Notification settings changed. Review your saved choices and try again.",
      422: "Check the saved notification settings before sending them.",
    });
  }
  if (subject === "event action") {
    Object.assign(messages, {
      400: "Check the saved event request before sending it.",
      403: "You do not have permission to change this event.",
      409: "This event changed. Your request is saved; use latest version to review its visibility.",
    });
  }
  if (subject === "document action") {
    Object.assign(messages, {
      400: "This document request needs a correction. Its action and versions are saved.",
      401: "Sign in again to send this document request.",
      403: "You do not have permission to change this document.",
      409: "This document changed. Review the saved request; use latest version to refresh its base.",
      422: "Check the saved document request before sending it.",
    });
  }
  if (subject === "topic edit") {
    Object.assign(messages, {
      400: "These project edits need a correction before they can be saved.",
      401: "Sign in again to save these project edits.",
      403: "You do not have permission to edit this project.",
      404: "This project is no longer available. Your edits are saved here.",
      409: "This project changed. Your edits are saved. Use latest version to review and save them.",
      422: "Check the title and summary before saving these project edits.",
    });
  }
  if (subject === "grouping change") {
    Object.assign(messages, {
      400: "This initiative can no longer take the card. It may be closed, or grouping would make a loop. The change is saved here.",
      401: "Sign in again to send this grouping change.",
      403: "You do not have permission to change this initiative.",
      404: "This card or initiative is no longer available.",
      409: "This card or initiative changed. Review the saved grouping change.",
      422: "This grouping change needs a correction before it can be sent.",
    });
  }
  if (subject === "assignment") {
    Object.assign(messages, {
      400: "This assignment needs a correction before it can be sent.",
      401: "Sign in again to send this assignment.",
      403: "You do not have permission to assign this initiative.",
      409: "This initiative changed. Your selected person is saved. Review the assignment.",
      422: "This assignment needs a correction before it can be sent.",
    });
  }
  return {
    state: retryable ? "retrying" : "attention",
    message: retryable
      ? subject === "inbox action"
        ? "The service could not confirm the inbox request. Its original action and versions are saved on this device."
        : subject === "file"
          ? "The service could not confirm the upload. Your file is kept on this device."
          : subject === "move"
            ? "The service could not confirm the move. Your destination and evidence are kept on this device."
            : subject === "assignment"
              ? "The service could not confirm the assignment. Your selection is kept on this device."
              : subject === "document action"
                ? "The service could not confirm the document request. Its original actions and versions are kept on this device."
                : subject === "topic edit"
                  ? "The service could not confirm the project edits. Your title and summary are kept on this device."
                  : subject === "grouping change"
                    ? "The service could not confirm the grouping change. It is kept on this device and will be sent again."
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
