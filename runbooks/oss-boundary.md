## OSS boundary checks

`make oss-boundary-check` tests the guard and scans every tracked text file,
including the guard itself. Binary artifacts are ignored. `make check` includes
this target, and CI requires it through `ci-ok`.

The built-in patterns detect absolute macOS and Linux user-home paths, root
volume mounts, and the Tailnet hostname shape. Use environment-relative paths
such as `$HOME/project`, relative links, or neutral `/opt/example` paths in
examples. System library paths beneath `/System` are not root volume mounts.

Private identifiers must remain outside the repository. Supply additional
case-insensitive extended regular expressions through `OSS_BOUNDARY_EXTRA_PATTERNS`
(one expression per line) or `OSS_BOUNDARY_EXTRA_FILE` (a file outside the checkout).
Both sources are additive; blank lines are ignored. For local checks:

```sh
OSS_BOUNDARY_EXTRA_FILE=/path/outside/checkout/denylist.txt make oss-boundary-check
```

Maintainers configure **Settings → Secrets and variables → Actions → New
repository secret**, name it `OSS_BOUNDARY_DENYLIST`, and enter the newline-separated
expressions there. Never copy the values into code, tests, PR descriptions,
comments, or commit messages. Treat expressions as ERE syntax and escape regex
metacharacters when matching literal text.

CI supplies this secret on main pushes, merge-group checks, and same-repository
pull requests. Fork pull requests receive only the generic rules. An unset or
empty secret does not fail the check. Matches report source locations and text,
without printing the expression; invalid expressions and read/Git errors produce
a generic error and fail rather than silently disabling the guard.
