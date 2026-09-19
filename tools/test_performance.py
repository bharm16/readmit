"""Measurement summaries refuse absent/invalid observations."""
import unittest
import sys
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
