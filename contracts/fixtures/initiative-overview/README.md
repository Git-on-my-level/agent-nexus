These are serialized backend shapes for the initiative UI: `tile.json` is one
`OverviewInitiative`, `refs.json` is a `/refs/resolve` response, and `digest.json`
is an `/overview/changes` response. Core tests compare production serialization
against these fixtures, normalizing only resource identities, row order and
timestamps. UI tests can import them directly instead of inventing tile fields.

Native `url` values are workspace-relative. Hosted clients prepend their
`/o/<org>/w/<ws>` base to paths beginning with `/`; absolute HTTP(S) source URLs
are used as-is. Core never accepts an org, workspace slug or caller-supplied
origin to construct these URLs.
