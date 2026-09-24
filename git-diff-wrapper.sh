#!/bin/bash
set -euo pipefail

# Usage: CMDIFF_KEEP_UNCHANGED=1 git -c diff.external=git-diff-wrapper.sh log --ext-diff -p

# Git external diff wrapper: maps git's protocol args ($1 path, $2/$5 content files,
# $8 rename name) to `cmdiff -B $2 -A $5 a/$1 b/${8-$1}`; -B/-A carry the contents.

if [ "$#" -lt 7 ]; then
  echo "git-diff-wrapper.sh: expected Git external diff arguments" >&2
  exit 2
fi

old_name="$1"
old_content="$2"
# $3 = old hex, $4 = old mode
new_content="$5"
# $6 = new hex, $7 = new mode
# $8 = new file name (only present for renames); fall back to old name
new_name="${8:-$old_name}"

# Resolve the cmdiff binary relative to this script's location.
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cmdiff_bin="${script_dir}/cmdiff"
if [ ! -x "$cmdiff_bin" ]; then
  cmdiff_bin="$(command -v cmdiff 2>/dev/null || echo cmdiff)"
fi

# Skip-unchanged flag
skip_flags="--skip-unchanged"
if [ "${CMDIFF_KEEP_UNCHANGED-}" = "1" ]; then
  skip_flags=""
fi

# NO_COLOR (https://no-color.org): when present and non-empty, disable ANSI color.
no_color_flags=""
if [ -n "${NO_COLOR-}" ]; then
  no_color_flags="--no-color"
fi

# Added/removed body style flags (case separation for testing)
# CMDIFF_ADDED_STYLE=white|green, CMDIFF_REMOVED_STYLE=white|red
added_flags=""
if [ -n "${CMDIFF_ADDED_STYLE-}" ]; then
  added_flags="--added-style=$CMDIFF_ADDED_STYLE"
fi
removed_flags="--removed-style=red"
if [ -n "${CMDIFF_REMOVED_STYLE-}" ]; then
  removed_flags="--removed-style=$CMDIFF_REMOVED_STYLE"
fi

if [ -n "${CMDIFF_WORD_DIFF-}" ]; then
  word_diff="--word-diff --ignore-all-space"
else
  word_diff="--ignore-all-space"
fi

# /dev/null sides get an empty temp file so cmdiff can read them.
old_tmp=""
new_tmp=""
if [ "$old_content" = "/dev/null" ] || [ ! -f "$old_content" ]; then
  old_tmp=$(mktemp)
  old_content="$old_tmp"
fi
if [ "$new_content" = "/dev/null" ] || [ ! -f "$new_content" ]; then
  new_tmp=$(mktemp)
  new_content="$new_tmp"
fi

cleanup() {
  rm -f "$old_tmp" "$new_tmp"
}
trap cleanup EXIT

# No exec: the EXIT trap must remove the /dev/null temp files after cmdiff finishes.
"$cmdiff_bin" $no_color_flags $skip_flags $added_flags $removed_flags $word_diff \
    -B "$old_content" -A "$new_content" \
    "a/$old_name" "b/$new_name"
