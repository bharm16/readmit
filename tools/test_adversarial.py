"""The acceptance launcher must never turn missing/skipped evidence into a pass."""
import contextlib
import json
import os
import unittest

import adversarial


def events(*items):
    return '\n'.join(json.dumps(item) for item in items)


class EvidenceTests(unittest.TestCase):
    def test_requires_named_test_and_package_pass(self):
        good = events({'Action': 'pass', 'Test': 'TestBoundary'}, {'Action': 'pass'})
        self.assertEqual(adversarial.assess(good, ['TestBoundary']), [])
        for stream in ('', events({'Action': 'pass'}), events({'Action': 'pass', 'Test': 'TestOther'})):
            self.assertTrue(adversarial.assess(stream, ['TestBoundary']))

    def test_skipped_subtest_and_failed_package_are_not_acceptance(self):
        for action in ('skip', 'fail'):
            stream = events({'Action': action, 'Test': 'TestBoundary/permission'},
                            {'Action': 'pass', 'Test': 'TestBoundary'}, {'Action': 'pass'})
            self.assertTrue(adversarial.assess(stream, ['TestBoundary']))
        self.assertTrue(adversarial.assess(events({'Action': 'pass', 'Test': 'TestBoundary'}), ['TestBoundary']))

    def test_package_failure_wins_over_individual_passes(self):
        stream = events({'Action': 'pass', 'Test': 'TestBoundary'}, {'Action': 'fail'})
        self.assertTrue(adversarial.assess(stream, ['TestBoundary']))

    def test_invalid_output_is_not_evidence(self):
        self.assertTrue(adversarial.assess('not JSON', ['TestBoundary']))


@unittest.skipUnless(os.name == 'posix' and os.geteuid() != 0,
                     'launcher requires a non-root POSIX account')
class CandidateTests(unittest.TestCase):
    def test_dirty_candidate_is_refused_before_tests(self):
        with self.candidate() as (root, run):
            (root / 'untracked.py').write_text('# not committed\n')
            result, evidence = run('')
            self.assertNotEqual(result.returncode, 0)
            self.assertIn('clean committed candidate', result.stderr)
            self.assertFalse(evidence.exists())

    def test_changed_candidate_cannot_pass(self):
        for mutation in ('tracked', 'untracked', 'revision'):
            with self.subTest(mutation=mutation), self.candidate() as (_, run):
                result, evidence = run(mutation)
                self.assertNotEqual(result.returncode, 0)
                summary = (evidence / 'summary.txt').read_text()
                self.assertIn('FAIL source changed during acceptance', summary)
                self.assertIn('Local matrix: FAIL', summary)
                self.assertNotIn('Local matrix: PASS', summary)

    def test_clean_unchanged_candidate_keeps_finite_scope(self):
        with self.candidate() as (_, run):
            result, evidence = run('')
            self.assertEqual(result.returncode, 0, result.stderr)
            summary = (evidence / 'summary.txt').read_text()
            self.assertIn('Source at completion:', summary)
            self.assertIn('Local matrix: PASS; full #111 acceptance: NOT ESTABLISHED', summary)

    @staticmethod
    @contextlib.contextmanager
    def candidate():
        from pathlib import Path
        import subprocess
        import sys
        import tempfile
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            root = base / 'repo'
            (root / 'tools').mkdir(parents=True)
            (root / 'hub').mkdir()
            (root / 'tools/adversarial.py').write_bytes(Path(adversarial.__file__).read_bytes())
            def git(*args):
                return subprocess.run(['git', *args], cwd=root, check=True, capture_output=True)
            git('init', '-q')
            git('config', 'user.email', 'fixture@example.invalid')
            git('config', 'user.name', 'Acceptance fixture')
            git('add', '.')
            git('commit', '-qm', 'fixture')
            binary = base / 'bin'
            binary.mkdir()
            go = binary / 'go'
            go.write_text(f'#!{sys.executable}\n' + '''import json, os, pathlib, subprocess, sys
root = pathlib.Path(os.environ['FIXTURE_ROOT'])
action = os.environ['FIXTURE_MUTATION']
marker = root.parent / 'called'
if not marker.exists():
    marker.touch()
    if action == 'tracked':
        with (root / 'tools/adversarial.py').open('a') as output:
            output.write('\\n# changed during execution\\n')
    elif action == 'untracked':
        (root / 'injected.go').write_text('package injected\\n')
    elif action == 'revision':
        subprocess.run(['git', 'commit', '--allow-empty', '-qm', 'new candidate'], cwd=root, check=True)
for name in sys.argv[-1][2:-2].split('|'):
    print(json.dumps({'Action': 'pass', 'Test': name}))
print(json.dumps({'Action': 'pass'}))
''')
            go.chmod(0o700)
            evidence = base / 'evidence'
            def run(mutation):
                env = dict(os.environ, PATH=str(binary) + os.pathsep + os.environ['PATH'],
                           READMIT_HUB_TEST_SOCKET=str(base), READMIT_HUB_TEST_PORT='56111',
                           READMIT_HUB_TEST_USER='fixture', FIXTURE_ROOT=str(root), FIXTURE_MUTATION=mutation)
                result = subprocess.run([sys.executable, str(root / 'tools/adversarial.py'), '--output', str(evidence)],
                                        cwd=root, env=env, capture_output=True, text=True, timeout=30)
                return result, evidence
            yield root, run


if __name__ == '__main__':
    unittest.main()
