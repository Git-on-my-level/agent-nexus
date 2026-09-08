#!/usr/bin/env python3
"""Opt-in read-only source connectivity evidence; NOT Nexus collector proof."""
import argparse
import datetime
import json
import re
import shlex
import subprocess
import sys


def run_json(argv):
    result = subprocess.run(argv, capture_output=True, text=True, timeout=30)
    if result.returncode:
        return None, {"status": "failed", "exit_code": result.returncode,
                      "limitation": "Command failed; raw output withheld to avoid leaking source data"}
    try:
        return json.loads(result.stdout), None
    except ValueError:
        return None, {"status": "failed", "limitation": "Non-JSON source response; raw output withheld"}


def github(repo, number):
    if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repo):
        raise ValueError("GitHub repo must be OWNER/REPO")
    metadata, error = run_json(["gh", "api", "--method", "GET", f"repos/{repo}"])
    if error:
        return error
    result = {"status": "read", "repository": repo, "visibility": metadata.get("visibility"),
              "credential_read_only": not any(metadata.get("permissions", {}).get(k)
                                              for k in ("admin", "maintain", "push", "triage")),
              "limitations": ["Existing CLI credentials; API read is not imported work or least-privilege enforcement"]}
    if number:
        issue, error = run_json(["gh", "api", "--method", "GET", f"repos/{repo}/issues/{number}"])
        if error:
            return error
        result["issue"] = {key: issue.get(key) for key in ("number", "html_url", "state", "updated_at")}
        result["issue"]["kind"] = "pull_request" if "pull_request" in issue else "issue"
    return result


def multica(profile, workspace, issue_id):
    command = ["multica"]
    if profile:
        command += ["--profile", profile]
    if workspace:
        command += ["--workspace-id", workspace]
    payload, error = run_json(command + ["issue", "inspect", issue_id, "--output", "json"])
    if error:
        return error
    issue = payload.get("issue", {})
    return {"status": "read", "issue": {key: issue.get(key) for key in ("id", "identifier", "status", "updated_at")},
            "has_latest_run": isinstance(payload.get("latest_run"), dict),
            "active_run_count": len(payload.get("active_runs") or []),
            "linked_pr_count": len(payload.get("pull_requests") or []),
            "limitations": ["Inspect metadata only; no new collector import, remote CLI parity or source write tested"]}


def ssh(host, path):
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.@-]*", host):
        raise ValueError("SSH host must be an approved hostname/alias, optionally user@host")
    if not path.startswith("/") or any(c in path for c in "\x00\r\n"):
        raise ValueError("SSH repo must be an approved absolute path")
    # SSH sends a shell command remotely. shlex.quote is required, not JSON
    # serialization or naive interpolation. All verbs and options are fixed.
    remote = "git --no-optional-locks -C " + shlex.quote(path) + " rev-parse --show-toplevel HEAD"
    command = ["ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes",
               "-o", "ConnectTimeout=8", "-o", "ConnectionAttempts=1", "-o", "ControlMaster=no",
               "-o", "ControlPath=none", "-o", "ForwardAgent=no", "-o", "ClearAllForwardings=yes",
               host, remote]
    result = subprocess.run(command, capture_output=True, text=True, timeout=20)
    lines = result.stdout.strip().splitlines()
    if result.returncode or len(lines) != 2 or not re.fullmatch(r"[0-9a-f]{40,64}", lines[-1]):
        return {"status": "failed", "exit_code": result.returncode,
                "host": host, "requested_path": path,
                "limitation": "SSH Git revision read failed or returned unexpected shape; raw output withheld"}
    return {"status": "read", "host": host, "requested_path": path,
            "resolved_repository": lines[0], "head": lines[1],
            "limitations": ["Git code-state evidence only, not deployment or accepted outcome",
                            "Source permission scope not proven by successful SSH authentication"]}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--github-repo")
    parser.add_argument("--github-issue", type=int)
    parser.add_argument("--multica-profile")
    parser.add_argument("--multica-workspace")
    parser.add_argument("--multica-issue")
    parser.add_argument("--ssh-host")
    parser.add_argument("--ssh-repo")
    args = parser.parse_args()
    if not any((args.github_repo, args.multica_issue, args.ssh_host)):
        parser.error("Select at least one approved source")
    if bool(args.ssh_host) != bool(args.ssh_repo):
        parser.error("--ssh-host and --ssh-repo are required together")
    if args.github_issue is not None and (not args.github_repo or args.github_issue < 1):
        parser.error("--github-issue needs --github-repo and a positive issue number")
    checks = {}
    for name, enabled, fn in (
        ("github", args.github_repo, lambda: github(args.github_repo, args.github_issue)),
        ("multica", args.multica_issue, lambda: multica(args.multica_profile, args.multica_workspace, args.multica_issue)),
        ("ssh_git", args.ssh_host, lambda: ssh(args.ssh_host, args.ssh_repo)),
    ):
        if enabled:
            try:
                checks[name] = fn()
            except (OSError, subprocess.TimeoutExpired, ValueError, TypeError, AttributeError) as exc:
                checks[name] = {"status": "failed", "limitation": type(exc).__name__}
    print(json.dumps({"schema_version": 1, "evidence_class": "real_source_read_only_connectivity",
                      "observed_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
                      "collector_import_verified": False, "checks": checks}, indent=2))
    return 0 if all(check["status"] == "read" for check in checks.values()) else 1


if __name__ == "__main__":
    sys.exit(main())
