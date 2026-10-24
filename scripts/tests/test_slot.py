"""Host slot wrapper regressions: capacity resolution, exclusion, release on kill."""

from __future__ import annotations

import importlib.machinery
import importlib.util
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import patch


HERE = Path(__file__).resolve().parent
SCRIPT = HERE.parent / "slot"

_loader = importlib.machinery.SourceFileLoader("anx_slot", str(SCRIPT))
_spec = importlib.util.spec_from_loader("anx_slot", _loader)
slot = importlib.util.module_from_spec(_spec)
_loader.exec_module(slot)


def wait_until(predicate, timeout=5.0):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if predicate():
            return True
        time.sleep(0.05)
    return False


class SlotTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.home = self.root / "home"
        self.home.mkdir()
        # Every wrapper runs with its own slot directory and config, so tests
        # neither touch the developer's real slots nor each other.
        self.env = {key: value for key, value in os.environ.items()
                    if not key.startswith("ANX_SLOTS")}
        self.env.update(TMPDIR=str(self.root), HOME=str(self.home))
        self.env.pop("XDG_RUNTIME_DIR", None)

    def slot_run(self, *argv, class_name="tests", **kwargs):
        return subprocess.Popen(
            [sys.executable, str(SCRIPT), "run", class_name, "--", *argv],
            env=self.env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True,
            **kwargs)

    def gated_holder(self, marker):
        """A command that proves it started, then holds its slot until released."""
        script = (f'touch "{marker}.started"\n'
                  f'while [ ! -e "{marker}" ]; do sleep 0.05; done\n')
        return self.slot_run("sh", "-c", script)

    def release(self, holders, markers):
        for marker in markers:
            Path(marker).write_text("")
        for process in holders:
            output, _ = process.communicate(timeout=10)
            self.assertEqual(process.returncode, 0, output)

    def test_passes_output_and_exit_status_through_without_waiting(self):
        done = self.slot_run("sh", "-c", "echo out; echo err >&2; exit 7")
        output, _ = done.communicate(timeout=10)
        self.assertEqual(done.returncode, 7)
        self.assertIn("out", output)
        self.assertIn("err", output)
        self.assertNotIn("waiting", output)
        # The lock file lands in the per-user slot directory, not beside the repo.
        self.assertTrue((self.root / "anx-slots" / "tests" / "0.lock").is_file())

    def test_capacity_env_then_file_then_default(self):
        config = self.home / ".config"
        config.mkdir()
        (config / "hostslots.json").write_text(json.dumps({"tests": 3, "e2e": 5}))
        default = max(1, (os.cpu_count() or 1) // 6)
        clean = {key: value for key, value in os.environ.items()
                 if not key.startswith("ANX_SLOTS")}
        clean["HOME"] = str(self.home)
        with patch.dict(os.environ, clean, clear=True):
            self.assertEqual(slot.capacity("tests"), 3)
            self.assertEqual(slot.capacity("e2e"), 5)
            with patch.dict(os.environ, {"ANX_SLOTS_TESTS": "5"}):
                self.assertEqual(slot.capacity("tests"), 5)
            # Anything that is not a positive integer is treated as absent.
            for raw in ("0", "-2", "two", ""):
                with patch.dict(os.environ, {"ANX_SLOTS_TESTS": raw}):
                    self.assertEqual(slot.capacity("tests"), 3, raw)
            (config / "hostslots.json").write_text("not json")
            self.assertEqual(slot.capacity("tests"), default)
            with patch.dict(os.environ, {"ANX_SLOTS_MY_CLASS": "4"}):
                self.assertEqual(slot.capacity("my-class"), 4)

    def test_slot_directory_is_per_user_and_env_driven(self):
        with patch.dict(os.environ, {"XDG_RUNTIME_DIR": "/run/user/1000"}):
            self.assertEqual(slot.slot_dir("tests"), Path("/run/user/1000/anx-slots/tests"))
        environ = {key: value for key, value in os.environ.items()
                   if key not in ("XDG_RUNTIME_DIR", "TMPDIR")}
        with patch.dict(os.environ, environ, clear=True):
            self.assertEqual(slot.slot_dir("tests"),
                             Path(f"/tmp/anx-{os.getuid()}/anx-slots/tests"))

    def test_one_slot_serializes_two_commands_with_a_single_wait_line(self):
        self.env["ANX_SLOTS_TESTS"] = "1"
        first = self.gated_holder(self.root / "gate")
        self.assertTrue(wait_until(lambda: (self.root / "gate.started").exists()))
        second = self.slot_run("sh", "-c", f'touch "{self.root / "second"}"')
        self.assertFalse(wait_until(lambda: (self.root / "second").exists(), 0.5),
                         "second command must wait for the held slot")
        self.release([first], [self.root / "gate"])
        self.assertTrue(wait_until(lambda: (self.root / "second").exists()))
        output, _ = second.communicate(timeout=10)
        self.assertEqual(second.returncode, 0)
        notices = [line for line in output.splitlines() if "waiting" in line]
        self.assertEqual(len(notices), 1, output)

    def test_capacity_two_admits_two_concurrent_holders(self):
        self.env["ANX_SLOTS_TESTS"] = "2"
        markers = [self.root / f"m{i}" for i in range(2)]
        holders = [self.gated_holder(marker) for marker in markers]
        self.assertTrue(wait_until(
            lambda: all(Path(f"{marker}.started").exists() for marker in markers), 10),
            "both holders must run concurrently under a capacity of two")
        self.release(holders, markers)

    def test_a_killed_holder_releases_its_slot(self):
        self.env["ANX_SLOTS_TESTS"] = "1"
        holder = self.slot_run("sh", "-c", f'touch "{self.root / "held"}"; sleep 10')
        # The holder's command only runs once the slot is taken.
        self.assertTrue(wait_until(lambda: (self.root / "held").exists()))
        waiter = self.slot_run("sh", "-c", f'touch "{self.root / "resumed"}"')
        self.assertFalse(wait_until(lambda: (self.root / "resumed").exists(), 0.5),
                         "the waiter must block while the holder lives")
        holder.kill()  # SIGKILL: the kernel must close the lock fd for us.
        holder.wait(timeout=10)
        self.assertTrue(wait_until(lambda: (self.root / "resumed").exists()),
                        "the waiter must proceed once the holder is killed")
        output, _ = waiter.communicate(timeout=10)
        self.assertIn("waiting", output)

    def test_the_wrapper_forwards_termination_signals_to_its_child(self):
        child_script = (f'trap \'touch "{self.root / "terminated"}"; exit 0\' TERM\n'
                        f'touch "{self.root / "ready"}"\n'
                        "while true; do sleep 0.05; done\n")
        wrapper = self.slot_run("sh", "-c", child_script)
        self.assertTrue(wait_until(lambda: (self.root / "ready").exists()),
                        "the child must install its trap and start")
        wrapper.send_signal(signal.SIGTERM)
        self.assertTrue(wait_until(lambda: (self.root / "terminated").exists(), 5),
                        "the child must receive the wrapper's SIGTERM")
        wrapper.communicate(timeout=10)
        self.assertEqual(wrapper.returncode, 0)

    def test_usage_errors_exit_two(self):
        for argv in ((), ("run",), ("run", "tests"), ("run", "tests", "--"),
                     ("run", "tests", "cmd"), ("take", "tests", "--", "cmd")):
            result = subprocess.run([sys.executable, str(SCRIPT), *argv],
                                    capture_output=True, text=True)
            self.assertEqual(result.returncode, 2, argv)
            self.assertIn("slot run", result.stderr, argv)


if __name__ == "__main__":
    unittest.main()
