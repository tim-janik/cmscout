#!/usr/bin/env python3
# This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

import argparse
import os
import re
import subprocess
import sys


def git (*args):
  return subprocess.check_output (['git', *args], text = True).strip()


def main ():
  parser = argparse.ArgumentParser (description = 'Require evidence for larger committed code removals.')
  parser.add_argument ('--base', default = os.environ.get ('CHECK_BASE') or None)
  parser.add_argument ('--head', default = os.environ.get ('CHECK_HEAD') or 'HEAD')
  args = parser.parse_args()
  inside = subprocess.run (['git', 'rev-parse', '--is-inside-work-tree'], capture_output = True, text = True)
  if inside.returncode or inside.stdout.strip() != 'true':
    if args.base or args.head != 'HEAD':
      parser.error ('cannot check an explicit range without Git history')
    print ('check-removals: skipped outside a Git checkout')
    return 0
  if not args.base:
    for ref in ('refs/remotes/origin/trunk', 'refs/heads/trunk'):
      if subprocess.run (['git', 'show-ref', '--verify', '--quiet', ref]).returncode == 0:
        args.base = ref
        break
    else:
      parser.error ('missing trunk ref; set CHECK_BASE or pass --base')
  revisions = []
  for ref in (args.base, args.head):
    result = subprocess.run (['git', 'rev-parse', '--verify', '--end-of-options', ref + '^{commit}'],
                             capture_output = True, text = True)
    if result.returncode:
      parser.error (f'unknown commit: {ref}')
    revisions.append (result.stdout.strip())
  base, head = revisions
  commits = git ('rev-list', '--reverse', '--no-merges', base + '..' + head).splitlines()
  print (f'check-removals: {base}..{head}, {len (commits)} commits')
  status = 0
  for commit in commits:
    changes = git ('diff-tree', '--root', '--no-commit-id', '--numstat', '-r', '-M', commit, '--',
                   '*.go', '*.sh', '*.py', '*.mk', 'Makefile', '*.yml', '*.yaml',
                   ':!*_test.go', ':!test_*.py', ':!*/test_*.py', ':!*-test.sh', ':!*/tests/**',
                   ':!testdata/**', ':!*.g.*')
    removed = 0
    for line in changes.splitlines():
      added, deleted, _path = line.split ('\t', 2)
      if added != '-' and deleted != '-':
        removed += max (0, int (deleted) - int (added))
    if removed < 10:
      continue
    message = git ('show', '-s', '--format=%B', commit)
    if re.search (r'^Evidence:[ \t]*\S', message, re.MULTILINE):
      continue
    subject = git ('show', '-s', '--format=%h %s', commit)
    print (f'{subject}: removes {removed} lines of code without nonempty Evidence:', file = sys.stderr)
    status = 1
  return status


if __name__ == '__main__':
  sys.exit (main())
