# This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

from pathlib import Path
import os
import subprocess
import sys
import tempfile
import unittest


ROOT = Path (__file__).resolve().parent.parent


class CheckFixture (unittest.TestCase):
  def setUp (self):
    temporary = tempfile.TemporaryDirectory()
    self.addCleanup (temporary.cleanup)
    self.root = Path (temporary.name)
    self.env = dict (os.environ, GIT_CONFIG_GLOBAL = '/dev/null', GIT_CONFIG_NOSYSTEM = '1',
                     GIT_AUTHOR_NAME = 'Test', GIT_AUTHOR_EMAIL = 'test@example.invalid',
                     GIT_COMMITTER_NAME = 'Test', GIT_COMMITTER_EMAIL = 'test@example.invalid')
    for name in ('GIT_DIR', 'GIT_WORK_TREE', 'GIT_INDEX_FILE', 'CHECK_BASE', 'CHECK_HEAD'):
      self.env.pop (name, None)
    self.write ('Makefile', 'all: build\nbuild check test:\nvariable := value\n')
    self.write ('.version', 'v1.0.0 2026-10-16 00:00:00 +0000\n')

  def write (self, path, text):
    target = self.root / path
    target.parent.mkdir (parents = True, exist_ok = True)
    target.write_text (text)

  def run_command (self, *args, cwd = None):
    return subprocess.run (args, cwd = cwd or self.root, env = self.env, capture_output = True, text = True)

  def git (self, *args):
    result = self.run_command ('git', *args)
    self.assertEqual (result.returncode, 0, result.stderr)
    return result.stdout.strip()

  def commit (self, message):
    self.git ('add', '-A')
    self.git ('commit', '-qm', message)


class AgentsMdTests (CheckFixture):
  def run_check (self, text):
    self.write ('AGENTS.md', text)
    return self.run_command (sys.executable, str (ROOT / 'misc/check-agents-md.py'), 'AGENTS.md')

  def test_valid_paths_and_targets_without_git (self):
    self.write ('pkg/ir/types.go', 'package ir\n')
    result = self.run_check ('`pkg/ir/` `pkg/ir/types.go` `Makefile` `make` `make check test`\n')
    self.assertEqual (result.returncode, 0, result.stderr)

  def test_missing_paths_are_reported_once (self):
    result = self.run_check ('`pkg/ir/missing.go` `README.md` `README.md`\n')
    self.assertEqual (result.returncode, 1)
    self.assertEqual (len (result.stderr.splitlines()), 2)
    self.assertIn ('missing path: pkg/ir/missing.go', result.stderr)
    self.assertIn ('missing path: README.md', result.stderr)

  def test_targets_are_literal_rules (self):
    for target in ('missing', 'check.extra', 'variable'):
      with self.subTest (target = target):
        result = self.run_check (f'`make {target}`\n')
        self.assertEqual (result.returncode, 1)
        self.assertIn (f'missing make target: {target}', result.stderr)

  def test_source_names_and_sample_syntax_are_ignored (self):
    result = self.run_check ('`ir.SemanticBlock` `foo()` `Evidence:` `CGO_ENABLED=1` `https://example.invalid/`\n')
    self.assertEqual (result.returncode, 0, result.stderr)

  def test_missing_document_fails (self):
    result = self.run_command (sys.executable, str (ROOT / 'misc/check-agents-md.py'), 'missing.md')
    self.assertNotEqual (result.returncode, 0)


class RemovalsTests (CheckFixture):
  def setUp (self):
    super().setUp()
    source = 'package example\n' + ''.join (f'var n{i} = {i}\n' for i in range (11))
    for path in ('pkg/example/example.go', 'pkg/example/other.go', 'pkg/example/example_test.go',
                 'pkg/example/generated.g.go', 'testdata/source.go', 'misc/test_helper.py'):
      self.write (path, source)
    self.git ('init', '-q', '-b', 'trunk')
    self.commit ('baseline')
    self.base = self.git ('rev-parse', 'HEAD')
    self.git ('checkout', '-qb', 'branch')

  def run_check (self, *args, cwd = None):
    return self.run_command (sys.executable, str (ROOT / 'misc/check-removals.py'), *args, cwd = cwd)

  def remove_code (self, message):
    self.write ('pkg/example/example.go', '')
    self.commit (message)

  def test_go_removal_needs_evidence (self):
    self.remove_code ('pkg/example/example.go: remove code')
    result = self.run_check()
    self.assertEqual (result.returncode, 1)
    self.assertIn ('removes 12 lines of code', result.stderr)

  def test_nonempty_evidence_passes (self):
    self.remove_code ('pkg/example/example.go: remove code\n\nEvidence: supersedes ' + self.base)
    result = self.run_check ('--base', self.base, '--head', 'HEAD')
    self.assertEqual (result.returncode, 0, result.stderr)
    self.assertIn (self.base, result.stdout)

  def test_empty_evidence_fails (self):
    self.remove_code ('pkg/example/example.go: remove code\n\nEvidence: \t')
    self.assertEqual (self.run_check().returncode, 1)

  def test_additions_in_another_file_do_not_cancel_removals (self):
    self.write ('pkg/example/additions.go', 'package example\n' + 'var added = 1\n' * 20)
    self.remove_code ('pkg/example/: replace code')
    self.assertEqual (self.run_check().returncode, 1)

  def test_ignores_tests_fixtures_and_generated_files (self):
    for path in ('pkg/example/example_test.go', 'pkg/example/generated.g.go', 'testdata/source.go', 'misc/test_helper.py'):
      self.write (path, '')
    self.commit ('tests/: remove cases')
    result = self.run_check()
    self.assertEqual (result.returncode, 0, result.stderr)

  def test_small_removal_passes (self):
    self.write ('pkg/example/example.go', 'package example\nvar n0 = 0\nvar n1 = 1\n')
    self.commit ('pkg/example/example.go: remove nine lines')
    self.assertEqual (self.run_check().returncode, 0)

  def test_rename_keeps_code (self):
    self.git ('mv', 'pkg/example/example.go', 'pkg/example/renamed.go')
    self.commit ('pkg/example/renamed.go: rename file')
    self.assertEqual (self.run_check().returncode, 0)

  def test_missing_base_in_checkout_fails (self):
    self.git ('branch', '-D', 'trunk')
    result = self.run_check()
    self.assertNotEqual (result.returncode, 0)
    self.assertIn ('missing trunk ref', result.stderr)

  def test_unknown_revisions_fail (self):
    for option in ('--base', '--head'):
      with self.subTest (option = option):
        result = self.run_check (option, 'missing')
        self.assertNotEqual (result.returncode, 0)
        self.assertIn ('unknown commit: missing', result.stderr)

  def test_environment_selects_range_and_options_override_it (self):
    self.env['CHECK_BASE'] = 'missing'
    self.assertIn ('unknown commit: missing', self.run_check().stderr)
    self.assertEqual (self.run_check ('--base', self.base).returncode, 0)

  def test_archive_reports_skipped_history_check (self):
    with tempfile.TemporaryDirectory() as archive:
      result = self.run_check (cwd = archive)
      self.assertEqual (result.returncode, 0, result.stderr)
      self.assertIn ('skipped outside a Git checkout', result.stdout)
      self.assertNotEqual (self.run_check ('--base', self.base, cwd = archive).returncode, 0)
      self.assertNotEqual (self.run_check ('--head', self.base, cwd = archive).returncode, 0)


class LintTests (CheckFixture):
  def setUp (self):
    super().setUp()
    self.git ('init', '-q', '-b', 'trunk')

  def assert_lint_fails (self, target):
    result = self.run_command ('make', '-f', str (ROOT / 'Makefile'), target)
    self.assertNotEqual (result.returncode, 0, result.stdout + result.stderr)

  def assert_lint_passes (self, target):
    result = self.run_command ('make', '-f', str (ROOT / 'Makefile'), target)
    self.assertEqual (result.returncode, 0, result.stdout + result.stderr)

  def test_unformatted_go_fails (self):
    self.write ('example.go', 'package example\nfunc f( ) { }\n')
    self.assert_lint_fails ('lint-go')
    self.write ('example.go', 'package example\n\nfunc f() {}\n')
    self.assert_lint_passes ('lint-go')

  def test_shell_syntax_error_fails (self):
    self.write ('example.sh', '#!/bin/bash\nif then\n')
    self.assert_lint_fails ('lint-shell')
    self.write ('example.sh', '#!/bin/bash\nexit 0\n')
    self.assert_lint_passes ('lint-shell')

  def test_python_syntax_error_fails (self):
    self.write ('example.py', 'def broken(:\n')
    self.assert_lint_fails ('lint-python')
    self.write ('example.py', 'def valid():\n  pass\n')
    self.assert_lint_passes ('lint-python')

  def test_invalid_workflow_fails (self):
    self.write ('.github/workflows/ci.yml', 'on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps: []\n')
    self.assert_lint_fails ('lint-workflows')
    self.write ('.github/workflows/ci.yml', 'on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n    - run: echo test\n')
    self.assert_lint_passes ('lint-workflows')

  def test_input_fixtures_are_not_linted (self):
    self.write ('testdata/broken.go', 'package broken\nfunc broken(\n')
    self.write ('testdata/broken.sh', '#!/bin/bash\nif then\n')
    self.write ('testdata/broken.py', 'def broken(:\n')
    for target in ('lint-go', 'lint-shell', 'lint-python'):
      with self.subTest (target = target):
        self.assert_lint_passes (target)


if __name__ == '__main__':
  unittest.main()
