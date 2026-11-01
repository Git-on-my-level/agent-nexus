"""Agent-guide regressions: length budgets with reasoned exceptions, live paths."""

import importlib.util
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location(
    "check_agent_guides", Path(__file__).resolve().parents[1] / "check-agent-guides.py")
guides = importlib.util.module_from_spec(spec)
spec.loader.exec_module(guides)


class GuideTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.git("init", "-q", "-b", "main")
        self.git("config", "user.name", "guide tests")
        self.git("config", "user.email", "guides@example.test")
        patcher = patch.object(guides, "ROOT", self.root)
        patcher.start()
        self.addCleanup(patcher.stop)
        # A minimal tree the guides can point at.
        for path in ("core/docs/runbook.md", "web-ui/src/lib/client.js"):
            (self.root / path).parent.mkdir(parents=True, exist_ok=True)
            (self.root / path).write_text("content\n")
        (self.root / "AGENTS.md").write_text("# AGENTS\n\nRoot guide.\n")
        self.git("add", ".")
        self.git("commit", "-qm", "fixture")

    def git(self, *args):
        env = {k: v for k, v in os.environ.items() if not k.startswith("GIT_")}
        env.update(GIT_CONFIG_GLOBAL=os.devnull, GIT_CONFIG_SYSTEM=os.devnull)
        return subprocess.check_output(["git", *args], cwd=self.root,
                                       stderr=subprocess.PIPE, env=env)

    def write_guide(self, path, text):
        file = self.root / path
        file.parent.mkdir(parents=True, exist_ok=True)
        file.write_text(text)
        self.git("add", ".")
        self.git("commit", "-qm", "guide")

    def problems(self):
        return guides.check_length(self.root) + guides.check_paths(self.root)

    def test_root_and_module_guides_get_120_lines_nested_get_60(self):
        body = ["# AGENTS", "", "Guide."] + [f"- rule {i}" for i in range(200)]
        self.write_guide("web-ui/AGENTS.md", "\n".join(body[:120]))
        self.assertEqual(self.problems(), [])
        self.write_guide("web-ui/AGENTS.md", "\n".join(body[:121]))
        self.assertEqual(self.problems(), [
            "web-ui/AGENTS.md is 121 lines, over its 120-line budget: trim it, or move "
            "detail into the deeper guide or doc that owns the topic"])
        self.write_guide("web-ui/AGENTS.md", "# AGENTS\n\nGuide.\n")
        self.write_guide("core/internal/router/AGENTS.md", "\n".join(body[:60]))
        self.assertEqual(self.problems(), [])
        self.write_guide("core/internal/router/AGENTS.md", "\n".join(body[:61]))
        self.assertIn("over its 60-line budget", self.problems()[0])

    def test_exceptions_hold_today_but_cannot_grow(self):
        with patch.object(guides, "LIMIT_EXCEPTIONS", {"web-ui/AGENTS.md": (130, "being trimmed")}):
            self.assertEqual(guides.limit_for("web-ui/AGENTS.md"), 130)
            body = ["# AGENTS", ""] + [f"- rule {i}" for i in range(200)]
            self.write_guide("web-ui/AGENTS.md", "\n".join(body[:130]))
            self.assertEqual(self.problems(), [])
            self.write_guide("web-ui/AGENTS.md", "\n".join(body[:131]))
            self.assertEqual(self.problems(), [
                "web-ui/AGENTS.md is 131 lines, over its 130-line budget "
                "(exception: being trimmed; do not add lines): trim it, or move "
                "detail into the deeper guide or doc that owns the topic"])

    def test_stated_paths_must_resolve_from_the_guide_or_the_root(self):
        self.write_guide("AGENTS.md", "\n".join([
            "# AGENTS",
            "",
            "See `core/docs/runbook.md` and [the client](web-ui/src/lib/client.js).",
            "Broken: `core/docs/missing.md` and [old](core/gone.md).",
        ]))
        self.assertEqual(self.problems(), [
            "AGENTS.md references `core/docs/missing.md`, which does not exist "
            "(from the guide's directory or the repository root)",
            "AGENTS.md references `core/gone.md`, which does not exist "
            "(from the guide's directory or the repository root)",
        ])

    def test_prose_and_placeholders_are_not_paths(self):
        self.write_guide("web-ui/AGENTS.md", "\n".join([
            "# AGENTS",
            "",
            "Run `make contract-gen` from scripts/slot.",
            "Skip `*_generated.go`, `$CONFIG/x.md`, `~/notes.md`, `../outside.md`,",
            "https://example.test/x.md and `stream.Mount` (a Go symbol).",
            "```",
            "do not check `core/docs/gone.md` inside a fenced block",
            "```",
            "After: `src/lib/client.js` resolves from this guide's directory.",
        ]))
        self.assertEqual(self.problems(), [])

    def test_allowances_and_anchors(self):
        with patch.object(guides, "PATH_ALLOWANCES", {"lib/client.js": "abbreviated path"}):
            self.write_guide("web-ui/AGENTS.md",
                             "# AGENTS\n\nUses `lib/client.js` and [setup](setup.md#first).\n")
            (self.root / "web-ui/setup.md").write_text("# Setup\n\nFirst.\n")
            self.git("add", ".")
            self.git("commit", "-qm", "setup")
            self.assertEqual(self.problems(), [])


if __name__ == "__main__":
    unittest.main()
