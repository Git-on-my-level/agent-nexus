import { describe, expect, it } from "vitest";

import {
  buildArtifactListQuery,
  buildArtifactListSearchString,
  formatArtifactTimestampInputValue,
  hasArtifactListFilters,
  parseArtifactListSearchParams,
} from "../../src/lib/artifactFilters.js";

describe("artifact list URL state", () => {
  it("parses artifact filters from search params", () => {
    const createdAfterIso = "2026-03-04T09:30:00.000Z";

    expect(
      parseArtifactListSearchParams(
        new URLSearchParams(
          `kind=receipt&thread_id=thread-onboarding&created_after=${encodeURIComponent(createdAfterIso)}`,
        ),
      ),
    ).toEqual({
      kind: "receipt",
      thread_id: "thread-onboarding",
      created_after: createdAfterIso,
      created_before: "",
    });
  });

  it("drops invalid values while parsing", () => {
    const createdBeforeIso = "2026-03-04T14:00:00.000Z";

    expect(
      parseArtifactListSearchParams(
        new URLSearchParams(
          `kind=unknown&thread_id=%20%20&created_after=not-a-date&created_before=${encodeURIComponent(createdBeforeIso)}`,
        ),
      ),
    ).toEqual({
      kind: "",
      thread_id: "",
      created_after: "",
      created_before: createdBeforeIso,
    });
  });

  it("serializes artifact filters into a stable search string", () => {
    const createdAfterLocal = "2026-03-04T09:30";
    const createdAfterIso = new Date(createdAfterLocal).toISOString();

    expect(
      buildArtifactListSearchString({
        kind: "receipt",
        thread_id: "thread-onboarding",
        created_after: createdAfterLocal,
        created_before: "",
      }),
    ).toBe(
      `kind=receipt&thread_id=thread-onboarding&created_after=${encodeURIComponent(createdAfterIso)}`,
    );
  });

  it("builds the artifact API query with ISO timestamps", () => {
    expect(
      buildArtifactListQuery({
        kind: "review",
        thread_id: "thread-onboarding",
        created_after: "2026-03-04T09:30",
        created_before: "2026-03-04T17:45",
      }),
    ).toEqual({
      kind: "review",
      thread_id: "thread-onboarding",
      created_after: new Date("2026-03-04T09:30").toISOString(),
      created_before: new Date("2026-03-04T17:45").toISOString(),
    });
  });

  it("formats canonical timestamps for datetime-local inputs", () => {
    const isoValue = "2026-03-04T09:30:00.000Z";
    const date = new Date(isoValue);
    const expected = [
      date.getFullYear(),
      String(date.getMonth() + 1).padStart(2, "0"),
      String(date.getDate()).padStart(2, "0"),
    ].join("-");
    const expectedTime = [
      String(date.getHours()).padStart(2, "0"),
      String(date.getMinutes()).padStart(2, "0"),
    ].join(":");

    expect(formatArtifactTimestampInputValue(isoValue)).toBe(
      `${expected}T${expectedTime}`,
    );
  });

  it("detects whether any artifact filters are active", () => {
    expect(hasArtifactListFilters({})).toBe(false);
    expect(hasArtifactListFilters({ thread_id: "thread-onboarding" })).toBe(
      true,
    );
  });
});
