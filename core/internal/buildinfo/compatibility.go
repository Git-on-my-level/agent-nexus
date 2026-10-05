package buildinfo

// MinCompatibleCLI is the wire-compatibility floor reported as min_cli_version.
// scripts/set-version.sh must not change this constant. Raise it only in a
// release that breaks CLI/core wire compatibility, and record that break in
// runbooks/release.md. recommended_cli_version tracks buildinfo.Current.
const MinCompatibleCLI = "v0.1.0"
