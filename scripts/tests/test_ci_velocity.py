"""Coverage and adversarial content checks for CI partition/release shortcuts."""
import contextlib
import importlib.util
import io
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[2]


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


core = load('ci_core', ROOT / 'scripts/ci-core.py')


class CorePartitionTest(unittest.TestCase):
    def test_discovery_success_needs_no_retry_and_filters_output(self):
        command = ['go', 'test', '-list', '.', './internal/server']
        result = subprocess.CompletedProcess(command, 0,
                                             'TestZulu\nTestAlpha\nTestAlpha\nExampleStore\nFuzzParser\nok package\n')
        with mock.patch.object(core.subprocess, 'run', return_value=result) as run:
            self.assertEqual(core.discover('./internal/server'),
                             ['ExampleStore', 'FuzzParser', 'TestAlpha', 'TestZulu'])
        run.assert_called_once_with(command, check=True, capture_output=True, text=True)

    def test_discovery_prints_failed_output_and_retries_once(self):
        command = ['go', 'test', '-list', '.', './internal/server']
        failure = subprocess.CalledProcessError(1, command, output='TestPartial\n',
                                               stderr='temporary build failure\n')
        result = subprocess.CompletedProcess(command, 0, 'TestRecovered\n')
        diagnostics = io.StringIO()
        with mock.patch.object(core.subprocess, 'run', side_effect=[failure, result]) as run:
            with contextlib.redirect_stderr(diagnostics):
                self.assertEqual(core.discover('./internal/server'), ['TestRecovered'])
        self.assertEqual(run.call_args_list,
                         [mock.call(command, check=True, capture_output=True, text=True)] * 2)
        self.assertIn('TestPartial', diagnostics.getvalue())
        self.assertIn('temporary build failure', diagnostics.getvalue())
        self.assertIn('Retrying test discovery once.', diagnostics.getvalue())

    def test_discovery_second_failure_remains_fatal_and_prints_both_attempts(self):
        command = ['go', 'test', '-list', '.', './internal/server']
        first = subprocess.CalledProcessError(1, command, output='first stdout', stderr='first stderr')
        second = subprocess.CalledProcessError(2, command, output='second stdout', stderr='second stderr')
        diagnostics = io.StringIO()
        with mock.patch.object(core.subprocess, 'run', side_effect=[first, second]) as run:
            with contextlib.redirect_stderr(diagnostics):
                with self.assertRaises(subprocess.CalledProcessError) as raised:
                    core.discover('./internal/server')
        self.assertIs(raised.exception, second)
        self.assertEqual(run.call_count, 2)
        for output in ['first stdout', 'first stderr', 'second stdout', 'second stderr', 'exit 2']:
            self.assertIn(output, diagnostics.getvalue())

    def test_disjoint_exhaustive_and_stable_with_examples_fuzz_and_prefixes(self):
        names = ['TestOne', 'TestOneMore', 'Example', 'ExampleStore', 'FuzzParser']
        names += [f'TestCase{i}' for i in range(300)]
        plan = [core.shard_names(names, index, 3) for index in range(1, 4)]
        self.assertEqual(sorted(sum(plan, [])), sorted(names))
        self.assertEqual(len(set(sum(plan, []))), len(names))
        self.assertEqual(plan, [core.shard_names(list(reversed(names)), i, 3) for i in range(1, 4)])
        for shard in plan:
            regex = re.compile('^(' + '|'.join(shard) + ')$')
            self.assertEqual([name for name in sorted(names) if regex.search(name)], shard)

    def test_measured_exclusions_fail_closed_for_new_correctness_tests(self):
        for name in core.MEASURED:
            self.assertRegex(name, core.ADVISORY)
        for name in ['TestCorrectness', 'TestCorrectness/Performance',
                     'TestPerformanceWorkRejectsIndexedAggregateMutation',
                     'TestPerformanceResumeMarkersCannotExposeHiddenCursor',
                     'TestNewPerformanceCorrectness', 'ExamplePerformance']:
            self.assertNotRegex(name, core.ADVISORY)

    def test_other_lane_keeps_server_descendants_and_covers_every_package(self):
        packages = ['core/internal/server', 'core/internal/server/stream',
                    'core/internal/primitives', 'core/internal/primitives/subpackage',
                    'core/internal/storage', 'core/cmd/server']
        other = core.other_packages(packages)
        self.assertIn('core/internal/server/stream', other)
        self.assertEqual(sorted(other + [p for p in packages if p.endswith('/internal/server')
                                         or '/internal/primitives' in p]), sorted(packages))


class ReleaseContentTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.repo = Path(self.temp.name)
        self.env = {**os.environ, 'GIT_CONFIG_NOSYSTEM': '1', 'GIT_CONFIG_GLOBAL': os.devnull}
        self.git('init', '-b', 'main')
        self.git('config', 'user.email', 'ci@example.invalid')
        self.git('config', 'user.name', 'CI Test')
        files = subprocess.check_output(['bash', str(ROOT / 'scripts/version-managed-files.sh')], text=True).splitlines()
        files += ['scripts/' + name for name in ['set-version.sh', 'sync-version.sh', 'read-version.sh', 'version-managed-files.sh']]
        for name in files:
            dest = self.repo / name
            dest.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(ROOT / name, dest)
        self.git('add', '.')
        self.git('commit', '-m', 'initial')
        self.parent = self.git('rev-parse', 'HEAD').strip()
        subprocess.run(['bash', 'scripts/set-version.sh', 'v99.0.1'], cwd=self.repo,
                       env=self.env, check=True, stdout=subprocess.DEVNULL)

    def git(self, *args):
        return subprocess.check_output(['git', *args], cwd=self.repo, env=self.env, stderr=subprocess.DEVNULL, text=True)

    def detect(self):
        self.git('add', '.')
        self.git('commit', '-m', 'candidate')
        return subprocess.check_output(['python3', str(ROOT / 'scripts/ci-release-base.py'), 'HEAD'],
                                       cwd=self.repo, env=self.env, text=True).strip()

    def test_canonical_bump_inherits_exact_parent(self):
        self.assertEqual(self.detect(), self.parent)

    def test_package_code_change_cannot_hide_in_version_files(self):
        path = self.repo / 'web-ui/package.json'
        path.write_text(path.read_text().replace('"scripts": {', '"scripts": {"unsafe": "echo unsafe",'))
        self.assertEqual(self.detect(), '')

    def test_generated_go_code_change_cannot_hide_in_version_files(self):
        path = self.repo / 'core/internal/buildinfo/version_generated.go'
        path.write_text(path.read_text() + '\nfunc init() { panic("unsafe") }\n')
        self.assertEqual(self.detect(), '')

    def test_mode_change_runs_full_ci(self):
        (self.repo / 'VERSION').chmod(0o755)
        self.assertEqual(self.detect(), '')

    def test_extra_file_runs_full_ci(self):
        (self.repo / 'extra').write_text('change')
        self.assertEqual(self.detect(), '')

    def test_deleted_file_runs_full_ci(self):
        (self.repo / 'web-ui/src/lib/generated/version.js').unlink()
        self.assertEqual(self.detect(), '')

    def test_symlink_runs_full_ci(self):
        p = self.repo / 'web-ui/src/lib/generated/version.js'
        p.unlink()
        p.symlink_to('/dev/null')
        self.assertEqual(self.detect(), '')

    def test_strict_release_detection_error_cannot_inherit_skipped_green_ci(self):
        result = subprocess.run(['python3', str(ROOT / 'scripts/ci-release-base.py'),
                                 '--strict', 'missing'], cwd=self.repo, env=self.env,
                                text=True, capture_output=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('could not verify release ancestry', result.stderr)

    def test_no_parent_or_missing_revision_runs_full_ci(self):
        out = subprocess.check_output(['python3', str(ROOT / 'scripts/ci-release-base.py'), 'missing'],
                                      cwd=self.repo, env=self.env, text=True)
        self.assertEqual(out.strip(), '')


if __name__ == '__main__':
    unittest.main()
