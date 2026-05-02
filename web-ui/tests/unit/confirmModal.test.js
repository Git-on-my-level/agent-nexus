import { describe, expect, it } from "vitest";

import {
  closeConfirmModal,
  emptyConfirmModal,
  emptyMessageEventConfirmModal,
  emptyTimelineConfirmModal,
  openConfirmModal,
} from "../../src/lib/confirmModal.js";

describe("confirmModal helpers", () => {
  it("emptyConfirmModal matches closeConfirmModal cleared shape", () => {
    const opened = openConfirmModal(emptyConfirmModal(), {
      open: true,
      action: "trash",
      entityId: "x",
      bulkIds: ["a"],
    });
    expect(closeConfirmModal(opened)).toEqual(emptyConfirmModal());
  });

  it("emptyTimelineConfirmModal carries event fields", () => {
    expect(emptyTimelineConfirmModal()).toEqual({
      open: false,
      action: "",
      eventId: "",
      eventRawType: "",
      bulkIds: null,
    });
  });

  it("emptyMessageEventConfirmModal is minimal", () => {
    expect(emptyMessageEventConfirmModal()).toEqual({
      open: false,
      action: "",
      eventId: "",
    });
  });
});
