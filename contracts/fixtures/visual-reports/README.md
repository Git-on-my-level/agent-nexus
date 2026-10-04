This corpus is the shared visual-report contract. `reports.json` fixes recognized
and valid outcomes for full static, expressive and live documents and adversarial
mutations. `queries.json` fixes live-query acceptance, preserving decimal and
exponent spellings in raw JSON. `summaries.json` fixes the shared Markdown summary,
checklist progress and Needs projection, including fence character/length rules
and empty final checkboxes.

Go tests exercise all three files, including text and structured report parsing.
The CLI forwarding package also reads the report corpus. The CI conformance runner
compares the production Go validator with `web-ui/src/lib/visualReports.js` over
all report cases, and checks browser query acceptance. Both validators must match
the stored outcomes; do not regenerate expectations from either validator as part
of a test. Add a named regression case when changing the contract.
