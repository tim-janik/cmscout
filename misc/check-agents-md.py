#!/usr/bin/env python3
# This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

from pathlib import Path
import re
import sys


def check (document):
  targets = set()
  for line in Path ('Makefile').read_text().splitlines():
    rule = re.match (r'^([\w. -]+):(?!=)', line)
    if rule:
      targets.update (rule[1].split())
  errors = []
  for token in sorted (set (re.findall (r'`([^`\n]+)`', document.read_text()))):
    if token == 'make' or token.startswith ('make '):
      for target in token.split()[1:] or ['all']:
        if target not in targets:
          errors.append (f'{document}: missing make target: {target}')
    elif re.fullmatch (r'[\w./-]+', token) and (
        '/' in token or token == 'Makefile' or
        Path (token).suffix in ('.md', '.go', '.sh', '.py', '.yml', '.yaml', '.mod', '.sum')):
      if not Path (token).exists():
        errors.append (f'{document}: missing path: {token}')
  for error in errors:
    print (error, file = sys.stderr)
  return bool (errors)


if __name__ == '__main__':
  sys.exit (check (Path (sys.argv[1] if len (sys.argv) > 1 else 'AGENTS.md')))
