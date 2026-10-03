import { describe, expect, it } from "vitest";

import {
  VISUAL_REPORT_LIMITS,
  VISUAL_REPORT_STALE_AFTER_MS,
  VISUAL_REPORT_TYPES,
  filterReportPanels,
  getPanelFreshness,
  parseVisualReport,
  safeReportUrl,
} from "../../src/lib/visualReports.js";
import {
  VISUAL_REPORT_EXAMPLE_OBSERVED_AT,
  visualReportExample,
  visualReportExampleContent,
} from "../../src/lib/fixtures/visualReportExample.js";

const now = Date.parse(VISUAL_REPORT_EXAMPLE_OBSERVED_AT);
const copy = () => JSON.parse(visualReportExampleContent);
const parse = (report) => parseVisualReport(JSON.stringify(report));
const panelOf = (report, type) =>
  report.panels.find((panel) => panel.type === type);

function invalidAfter(change) {
  const report = copy();
  change(report);
  const parsed = parse(report);
  expect(parsed.recognized).toBe(true);
  expect(parsed.report).toBeNull();
  expect(parsed.errors.length).toBeGreaterThan(0);
  return parsed.errors;
}

describe("visual report document parser", () => {
  it("leaves Markdown guides containing report examples as ordinary documents", () => {
    const markdown =
      '# Authoring guide\n\n```json\n{"kind":"anx.visual-report","schema_version":1}\n```';
    expect(parseVisualReport(markdown)).toEqual({
      recognized: false,
      report: null,
      errors: [],
    });
    expect(
      parseVisualReport('```json\n{"kind":"anx.visual-report"}\n```')
        .recognized,
    ).toBe(false);
  });

  it("reserves the all-project filter sentinel", () => {
    const errors = invalidAfter((report) => {
      report.projects[0].id = "all";
      report.panels.forEach((panel) => {
        if (panel.project_id === "anx-rollout") panel.project_id = "all";
      });
    });
    expect(errors.join(" ")).toContain("reserved for filtering");
  });

  it("round-trips all six primitives without changing source data", () => {
    const result = parseVisualReport(visualReportExampleContent);
    expect(result).toEqual({
      recognized: true,
      report: visualReportExample,
      errors: [],
    });
    expect(new Set(result.report.panels.map((panel) => panel.type))).toEqual(
      new Set(VISUAL_REPORT_TYPES.slice(0, 6)),
    );
    expect(parse(result.report)).toEqual(result);
  });

  it.each([
    null,
    undefined,
    123,
    {},
    "",
    "# Ordinary document",
    "not JSON",
    "{}",
    "[]",
    '{"kind":"another.document"}',
  ])("leaves ordinary document content inspectable: %j", (content) => {
    expect(parseVisualReport(content)).toEqual({
      recognized: false,
      report: null,
      errors: [],
    });
  });

  it("recognizes malformed report JSON without including raw parse details", () => {
    expect(
      parseVisualReport(
        '{"kind":"anx.visual-report","schema_version":1,"title":"Broken report",',
      ),
    ).toEqual({
      recognized: true,
      report: null,
      errors: ["report: invalid JSON"],
    });
  });

  it.each([2, "1", null])(
    "rejects unsupported schema version %j",
    (version) => {
      expect(
        invalidAfter((report) => {
          report.schema_version = version;
        }).join(" "),
      ).toContain("unsupported version");
    },
  );

  it("rejects a recognized unknown report kind", () => {
    expect(
      invalidAfter((report) => {
        report.kind = "anx.visual-report.future";
      }).join(" "),
    ).toContain("unsupported visual report kind");
  });

  it("rejects unknown components and never evaluates executable-looking payloads", () => {
    const errors = invalidAfter((report) => {
      report.panels[0].type = "<script>secret-payload</script>";
      report.panels[0].data = { script: "secret-payload" };
    });
    expect(errors.join(" ")).not.toContain("secret-payload");
  });

  it.each([
    (report) => {
      report.script = "payload";
    },
    (report) => {
      report.projects[0].html = "payload";
    },
    (report) => {
      report.sources[0].fetch = true;
    },
    (report) => {
      report.panels[0].onClick = "payload";
    },
    (report) => {
      report.panels[0].data.html = "payload";
    },
    (report) => {
      panelOf(report, "metric-chart").data.points[0].style = "payload";
    },
  ])(
    "rejects extra fields instead of forwarding execution or rendering options",
    (change) => {
      expect(invalidAfter(change).join(" ")).toContain("unsupported fields");
    },
  );

  it("treats HTML-looking strings as literal data, never markup", () => {
    const report = copy();
    report.panels[0].data.text =
      '<img src="https://example.org/pixel" onerror="alert(1)">';
    const result = parse(report);
    expect(result.errors).toEqual([]);
    expect(result.report.panels[0].data.text).toBe(report.panels[0].data.text);
  });

  it.each([
    "title",
    "summary",
    "generated_at",
    "projects",
    "sources",
    "panels",
  ])("requires top-level %s", (key) => {
    invalidAfter((report) => {
      delete report[key];
    });
  });

  it.each([
    "id",
    "project_id",
    "type",
    "title",
    "author",
    "provenance",
    "observed_at",
    "freshness",
    "source_ids",
    "data",
  ])("requires panel %s", (key) => {
    invalidAfter((report) => {
      delete report.panels[0][key];
    });
  });

  it.each(["projects", "sources", "panels"])(
    "rejects duplicate %s ids",
    (key) => {
      expect(
        invalidAfter((report) => {
          report[key][1].id = report[key][0].id;
        }).join(" "),
      ).toContain("must be unique");
    },
  );

  it.each(["projects", "sources", "panels"])(
    "rejects nonidentifier %s ids",
    (key) => {
      invalidAfter((report) => {
        report[key][0].id = "bad id<script>";
      });
    },
  );

  it("rejects dangling project and source references", () => {
    invalidAfter((report) => {
      report.panels[0].project_id = "missing";
    });
    invalidAfter((report) => {
      report.panels[0].source_ids = ["missing"];
    });
  });

  it("requires row and milestone sources to be declared on their panel", () => {
    invalidAfter((report) => {
      panelOf(report, "evidence-table").source_ids = ["anx-release"];
    });
    invalidAfter((report) => {
      panelOf(report, "milestone-timeline").source_ids = ["anx-release"];
    });
  });

  it("rejects duplicate source references and verified panels without evidence", () => {
    invalidAfter((report) => {
      report.panels[0].source_ids = ["anx-release", "anx-release"];
    });
    invalidAfter((report) => {
      panelOf(report, "artifact-preview").source_ids = [];
    });
  });

  it.each(["healthy", "", null])(
    "rejects invalid freshness %j",
    (freshness) => {
      invalidAfter((report) => {
        report.panels[0].freshness = freshness;
      });
    },
  );

  it("requires an observation for current and stale panels", () => {
    for (const freshness of ["current", "stale"]) {
      invalidAfter((report) => {
        Object.assign(report.panels[0], { freshness, observed_at: null });
      });
    }
  });

  it.each([
    "2026-02-30T00:00:00Z",
    "2026-10-03",
    "2026-10-03T06:38:37",
    "2026-13-01T00:00:00Z",
    "2026-01-00T00:00:00Z",
    "2026-01-01T24:00:00Z",
    "2026-01-01T00:00:00+25:00",
    "not a date",
  ])("rejects invalid or ambiguous timestamps %s", (timestamp) => {
    invalidAfter((report) => {
      report.generated_at = timestamp;
    });
    invalidAfter((report) => {
      report.sources[0].observed_at = timestamp;
    });
    invalidAfter((report) => {
      report.panels[0].observed_at = timestamp;
    });
  });

  it("accepts explicit timezone offsets and unknown observations", () => {
    const report = copy();
    report.generated_at = "2026-10-03T08:38:37+02:00";
    report.sources[0].observed_at = null;
    expect(parse(report).errors).toEqual([]);
  });

  it("bounds document bytes before validation, including multibyte text", () => {
    const report = copy();
    report.summary = "é".repeat(VISUAL_REPORT_LIMITS.bytes / 2);
    const result = parse(report);
    expect(result.report).toBeNull();
    expect(result.errors).toEqual(["report: exceeds the 128 KiB size limit"]);
    expect(
      parseVisualReport("a".repeat(VISUAL_REPORT_LIMITS.bytes + 1)).recognized,
    ).toBe(false);
  });

  it("bounds error count and does not echo attacker-controlled keys", () => {
    const report = copy();
    report.panels = Array.from({ length: VISUAL_REPORT_LIMITS.panels }, () => ({
      secretPayload: true,
    }));
    const result = parse(report);
    expect(result.errors).toHaveLength(VISUAL_REPORT_LIMITS.errors);
    expect(result.errors.join(" ")).not.toContain("secretPayload");
  });

  it.each(["projects", "sources", "panels"])("bounds %s counts", (key) => {
    invalidAfter((report) => {
      report[key] = Array.from(
        { length: VISUAL_REPORT_LIMITS[key] + 1 },
        () => ({}),
      );
    });
  });

  it("bounds strings and rejects object-valued text", () => {
    invalidAfter((report) => {
      report.title = "a".repeat(VISUAL_REPORT_LIMITS.title + 1);
    });
    invalidAfter((report) => {
      report.panels[0].data.text = "a".repeat(VISUAL_REPORT_LIMITS.text + 1);
    });
    invalidAfter((report) => {
      report.panels[0].data.text = { html: "test" };
    });
  });
});

describe("visual report component validation", () => {
  it.each(["from", "to"])(
    "rejects deeply nested edge %s safely without recursively serializing data",
    (endpoint) => {
      const value = endpoint === "from" ? "release" : "qualification";
      const nested = "[".repeat(10000) + '"invalid"' + "]".repeat(10000);
      const content = visualReportExampleContent.replace(
        `"${endpoint}": "${value}"`,
        `"${endpoint}": ${nested}`,
      );
      let result;
      expect(() => {
        result = parseVisualReport(content);
      }).not.toThrow();
      expect(result.recognized).toBe(true);
      expect(result.report).toBeNull();
      expect(result.errors.join(" ")).toContain(
        `.${endpoint}: must be an identifier`,
      );
    },
  );

  it("requires matching table cells and bounded columns and rows", () => {
    invalidAfter((report) => {
      panelOf(report, "evidence-table").data.rows[0].cells.pop();
    });
    invalidAfter((report) => {
      panelOf(report, "evidence-table").data.columns = Array(
        VISUAL_REPORT_LIMITS.columns + 1,
      ).fill("Column");
    });
    invalidAfter((report) => {
      panelOf(report, "evidence-table").data.rows = Array(
        VISUAL_REPORT_LIMITS.rows + 1,
      ).fill({});
    });
  });

  it("requires bounded timeline entries with explicit status and evidence", () => {
    invalidAfter((report) => {
      panelOf(report, "milestone-timeline").data.items = Array(
        VISUAL_REPORT_LIMITS.milestones + 1,
      ).fill({});
    });
    invalidAfter((report) => {
      panelOf(report, "milestone-timeline").data.items[0].status = "healthy";
    });
    invalidAfter((report) => {
      panelOf(report, "milestone-timeline").data.items[0].source_ids = [
        "missing",
      ];
    });
  });

  it("rejects duplicate nodes and dangling, repeated, or self-referencing edges", () => {
    invalidAfter((report) => {
      const data = panelOf(report, "dependency-diagram").data;
      data.nodes[1].id = data.nodes[0].id;
    });
    invalidAfter((report) => {
      panelOf(report, "dependency-diagram").data.edges[0].to = "missing";
    });
    invalidAfter((report) => {
      const data = panelOf(report, "dependency-diagram").data;
      data.edges.push(data.edges[0]);
    });
    invalidAfter((report) => {
      const edge = panelOf(report, "dependency-diagram").data.edges[0];
      edge.to = edge.from;
    });
  });

  it("bounds diagram nodes and edges", () => {
    invalidAfter((report) => {
      panelOf(report, "dependency-diagram").data.nodes = Array(
        VISUAL_REPORT_LIMITS.nodes + 1,
      ).fill({});
    });
    invalidAfter((report) => {
      panelOf(report, "dependency-diagram").data.edges = Array(
        VISUAL_REPORT_LIMITS.edges + 1,
      ).fill({});
    });
  });

  it.each([-1, 1e13, "5", {}, true])(
    "rejects unsupported metric value %j",
    (value) => {
      invalidAfter((report) => {
        panelOf(report, "metric-chart").data.points[0].value = value;
      });
    },
  );

  it("rejects numeric overflow parsed from JSON", () => {
    const content = visualReportExampleContent.replace(
      '"value": 3',
      '"value": 1e400',
    );
    expect(parseVisualReport(content).report).toBeNull();
  });

  it("accepts zero, null unknown, and bounded finite metrics without fabrication", () => {
    const report = copy();
    panelOf(report, "metric-chart").data.points = [
      { label: "Zero", value: 0 },
      { label: "Unknown", value: null },
      { label: "Maximum", value: 1e12 },
    ];
    expect(parse(report).errors).toEqual([]);
  });

  it("bounds metrics and requires illustrative flags to agree", () => {
    invalidAfter((report) => {
      panelOf(report, "metric-chart").data.points = Array(
        VISUAL_REPORT_LIMITS.points + 1,
      ).fill({});
    });
    invalidAfter((report) => {
      panelOf(report, "metric-chart").data.illustrative = false;
    });
    invalidAfter((report) => {
      panelOf(report, "metric-chart").provenance = "reported";
    });
  });

  it("accepts text-only artifact previews and rejects executable/embedded formats", () => {
    for (const mediaType of [
      "text/html",
      "image/svg+xml",
      "image/png",
      "application/javascript",
      "application/pdf",
    ]) {
      invalidAfter((report) => {
        panelOf(report, "artifact-preview").data.media_type = mediaType;
      });
    }
    invalidAfter((report) => {
      panelOf(report, "artifact-preview").data.url = "javascript:alert(1)";
    });
  });
});

describe("visual report links", () => {
  it.each([
    "https://example.org/report#evidence",
    "http://example.org/report?q=1",
    "HTTPS://EXAMPLE.ORG/report",
  ])("accepts explicit HTTP(S) links %s", (url) => {
    expect(safeReportUrl(url)).toBe(new URL(url).href);
  });

  it.each([
    null,
    {},
    "",
    "//example.org",
    "/report",
    "javascript:alert(1)",
    "data:text/html,test",
    "file:///tmp/test",
    "https://user:pass@example.org",
    "https://user@example.org",
    "https://example.org\\@evil.example",
    "https://example.org/with space",
    "https://example.org/\npath",
    "https://example.org/\u0000",
    "https://",
  ])("rejects unsafe or ambiguous URL %j", (url) => {
    expect(safeReportUrl(url)).toBe("");
    invalidAfter((report) => {
      report.sources[0].url = url;
    });
  });

  it("bounds URL length", () => {
    expect(
      safeReportUrl(
        `https://example.org/${"a".repeat(VISUAL_REPORT_LIMITS.url)}`,
      ),
    ).toBe("");
  });
});

describe("visual report freshness and project filtering", () => {
  it("demotes current observations only after 24 hours", () => {
    const panel = visualReportExample.panels[0];
    expect(getPanelFreshness(panel, now)).toBe("current");
    expect(getPanelFreshness(panel, now + VISUAL_REPORT_STALE_AFTER_MS)).toBe(
      "current",
    );
    expect(
      getPanelFreshness(panel, now + VISUAL_REPORT_STALE_AFTER_MS + 1),
    ).toBe("stale");
  });

  it("preserves stale, unknown, and unavailable states", () => {
    const panel = visualReportExample.panels[0];
    for (const freshness of ["stale", "unknown", "unavailable"]) {
      expect(getPanelFreshness({ ...panel, freshness }, now)).toBe(freshness);
    }
    expect(
      getPanelFreshness({ freshness: "unavailable", observed_at: null }, now),
    ).toBe("unavailable");
  });

  it("fails safely on missing, invalid, future, or unrecognized observations", () => {
    const panel = visualReportExample.panels[0];
    for (const observed_at of [
      null,
      undefined,
      "invalid",
      "2026-11-03T00:00:00Z",
    ]) {
      expect(getPanelFreshness({ ...panel, observed_at }, now)).toBe("unknown");
    }
    expect(getPanelFreshness(panel, NaN)).toBe("unknown");
    expect(getPanelFreshness({ ...panel, freshness: "healthy" }, now)).toBe(
      "unknown",
    );
    expect(getPanelFreshness(null, now)).toBe("unknown");
  });

  it("filters projects without mixing actual and illustrative panels", () => {
    const actual = filterReportPanels(
      visualReportExample,
      "anx-rollout",
      "all",
      now,
    );
    const example = filterReportPanels(
      visualReportExample,
      "reporting-example",
      "all",
      now,
    );
    expect(actual).toHaveLength(6);
    expect(example.map((panel) => panel.id)).toEqual(["example-metrics"]);
    expect(
      filterReportPanels(visualReportExample, "missing", "all", now),
    ).toEqual([]);
    expect(filterReportPanels(null)).toEqual([]);
  });

  it("combines project and effective freshness filters without rewriting report state", () => {
    expect(
      filterReportPanels(
        visualReportExample,
        "anx-rollout",
        "unavailable",
        now,
      ).map((panel) => panel.id),
    ).toEqual(["unavailable-observations"]);
    expect(
      filterReportPanels(visualReportExample, "all", "unknown", now).map(
        (panel) => panel.id,
      ),
    ).toEqual(["example-metrics"]);
    expect(
      filterReportPanels(
        visualReportExample,
        "anx-rollout",
        "stale",
        now + VISUAL_REPORT_STALE_AFTER_MS + 1,
      ),
    ).toHaveLength(5);
    expect(visualReportExample.panels[0].freshness).toBe("current");
  });

  it("keeps public release claims separate from qualification and synthetic metrics", () => {
    expect(visualReportExample.projects[0].outcome).toBe(
      "Released; operational qualification pending",
    );
    expect(
      visualReportExample.panels
        .filter((panel) => panel.type === "metric-chart")
        .every(
          (panel) =>
            panel.project_id === "reporting-example" &&
            panel.provenance === "illustrative",
        ),
    ).toBe(true);
    expect(
      visualReportExample.sources.every(
        (source) =>
          source.url.startsWith("https://github.com/Git-on-my-level/") &&
          source.observed_at === VISUAL_REPORT_EXAMPLE_OBSERVED_AT,
      ),
    ).toBe(true);
    expect(visualReportExampleContent).toContain("agentctl v0.13.0");
  });
});
