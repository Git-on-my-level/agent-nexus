# Thread detail dataflow plan (superseded)

This document described a frontend refactor for monolithic thread detail loads (N+1 fetches, inconsistent refresh after mutations) in the pre-topic/card operator model.

**Current direction:** Operator workflows center on **topics**, **cards**, and **boards**. Thread routes remain for backing timelines and compatibility reads. Detail pages should orchestrate data through topic workspace / thread context projections and canonical topic and card patch flows rather than ad-hoc per-resource fan-out.

For authoritative behavior, see:

- `web-ui/docs/oar-ui-spec.md`
- `web-ui/docs/http-api.md`
- `/contracts/oar-openapi.yaml` (`threads.context`, `threads.workspace`, `topics/*`, `boards/*`, `cards/*`)
