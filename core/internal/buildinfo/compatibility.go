package buildinfo

// MinCompatibleCLI is the wire-compatibility floor reported as min_cli_version.
// scripts/set-version.sh must not change this constant. Raise it only in a
// release that breaks CLI/core wire compatibility, and record that break in
// runbooks/release.md. recommended_cli_version tracks buildinfo.Current.
//
// v0.11.0 is the oldest release that works against current core. v0.10.25
// still registers agents with POST /auth/agents/register, which v0.11.0
// removed, so that CLI would otherwise pass a v0.1.0 floor and then get 404.
const MinCompatibleCLI = "v0.11.0"
