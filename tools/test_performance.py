"""Measurement summaries refuse absent/invalid observations."""
import unittest
import sys
import tempfile
import subprocess
from unittest.mock import patch
from pathlib import Path
import performance


class Measurements(unittest.TestCase):
    def test_nearest_rank_does_not_average_tail(self):
        self.assertEqual(performance.p95(list(range(1, 21))), 19)

    def test_no_samples_are_not_a_passing_zero(self):
        for values in ([], [float('nan')], [-1], [float('inf')]):
            with self.assertRaises(ValueError):
                performance.p95(values)

    def test_rss_platform_units_and_missing_measurement(self):
        self.assertEqual(performance.rss_bytes(' 123 maximum resident set size\n', 'darwin'), 123)
        self.assertEqual(performance.rss_bytes('Maximum resident set size (kbytes): 123\n', 'linux'), 125952)
        for system, output in [('darwin', ''), ('linux', 'Maximum resident set size (kbytes): 0'), ('windows', '')]:
            with self.assertRaises(ValueError):
                performance.rss_bytes(output, system)

    @unittest.skipUnless(sys.platform in ('darwin', 'linux'), 'POSIX measurement process groups')
    def test_failed_or_timed_out_process_is_not_a_measurement(self):
        with self.assertRaises(RuntimeError):
            performance.run([sys.executable, '-c', 'raise SystemExit(2)'])
        with self.assertRaises(RuntimeError):
            performance.run([sys.executable, '-c', 'import time; time.sleep(10)'], timeout=.1)

    def test_oversized_import_requires_size_refusal_not_licensing_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for diagnostic in ('operation policy is required', '', 'cannot open source'):
                with self.assertRaises(RuntimeError):
                    performance.oversized_import(
                        [sys.executable, '-c', f'import sys; print({diagnostic!r}, file=sys.stderr); sys.exit(1)'],
                        root / 'corpus', root)
            performance.oversized_import(
                [sys.executable, '-c', 'import sys; print("a declared import location exceeds its size limit", file=sys.stderr); sys.exit(1)'],
                root / 'corpus', root)

    @unittest.skipUnless(sys.platform in ('darwin', 'linux'), 'POSIX measurement signals')
    def test_cancellation_refuses_command_failure_before_progress(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            with self.assertRaises(RuntimeError):
                performance.cancellation(
                    [sys.executable, '-c', 'import sys; print("operation refused", file=sys.stderr); sys.exit(1)'],
                    root / 'corpus', root / 'report')

    def test_import_timeout_does_not_disclose_operation_policy(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            command = ['readmit', '--operation-policy', '/private/policy-name']
            with patch.object(performance.subprocess, 'run', side_effect=subprocess.TimeoutExpired(command, 60)):
                with self.assertRaisesRegex(RuntimeError, '^oversized import could not be measured$'):
                    performance.oversized_import(command, root / 'corpus', root)

    def test_missing_policy_does_not_disclose_its_path(self):
        result = subprocess.run([sys.executable, str(Path(performance.__file__)),
                                 '--binary', sys.executable, '--operation-policy', '/not-present/private-policy-name'],
                                capture_output=True, text=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn('private-policy-name', result.stderr)

    @unittest.skipUnless(sys.platform in ('darwin', 'linux'), 'POSIX measurement signals')
    def test_cancellation_timeout_does_not_disclose_operation_policy(self):
        original = subprocess.Popen.communicate
        def timeout_when_bounded(process, *args, **kwargs):
            if kwargs.get('timeout') is not None:
                raise subprocess.TimeoutExpired(process.args, kwargs['timeout'])
            return original(process, *args, **kwargs)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            command = [sys.executable, '-c',
                       'import sys,time; print("scanning:", file=sys.stderr, flush=True); time.sleep(10)',
                       '--operation-policy', '/private/policy-name']
            with patch.object(subprocess.Popen, 'communicate', timeout_when_bounded):
                with self.assertRaisesRegex(RuntimeError, '^cancellation acknowledgement deadline exceeded$'):
                    performance.cancellation(command, root / 'corpus', root / 'report')
