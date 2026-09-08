#!/usr/bin/env python3

import argparse
import json
import sys


def component_index(snapshot):
    components = {}
    for component in snapshot["components"]:
        name = component["qualified_name"]
        if not isinstance(name, str) or not name or name in components:
            raise ValueError("missing or duplicate qualified name")
        components[name] = component
    return components


def known_number(value, label):
    if type(value) is not int:
        raise ValueError(f"unknown {label}")
    return value


def check(report, limits):
    if report["schema_version"] != "1" or report["status"] != "complete":
        raise ValueError("a complete comparison with schema 1 is required")
    before = component_index(report["before"])
    after = component_index(report["after"])
    failed = False
    for change in report["changes"]:
        old_name, new_name = change["before_name"], change["after_name"]
        if old_name is not None and old_name not in before:
            raise ValueError(f"missing before record: {old_name}")
        if new_name is None:
            continue
        if new_name not in after:
            raise ValueError(f"missing after record: {new_name}")
        component = after[new_name]
        if "cyclomatic" not in component:
            if component["kind"] in {"function", "method", "arrow_function", "lambda"}:
                raise ValueError(f"missing function metrics: {new_name}")
            continue
        if change["match"]["status"] not in {"matched", "added"}:
            raise ValueError(f"uncertain match: {new_name}")
        metric = component["cyclomatic"]
        if metric["status"] != "complete":
            raise ValueError(f"incomplete function metrics: {new_name}")
        value = known_number(metric["value"], "complexity")
        location = f'{report["after"]["path"]}:{component["span"]["start_line"] + 1}'
        if limits.max_complexity is not None and value > limits.max_complexity:
            print(f"{location}: {new_name}: complexity {value} exceeds {limits.max_complexity}")
            failed = True
        if limits.max_increase is not None and old_name is not None:
            if change["delta"]["status"] != "complete":
                raise ValueError(f"complexity delta is not comparable: {new_name}")
            increase = known_number(change["delta"]["cyclomatic"], "complexity delta")
            if increase > limits.max_increase:
                print(f"{location}: {new_name}: complexity increased by {increase}, limit {limits.max_increase}")
                failed = True
        if limits.max_comment_chars is not None:
            if component["comment_status"] != "complete" or component["prefix_comment"] is None:
                raise ValueError(f"unknown comment ownership: {new_name}")
            comments = [("prefix", component["prefix_comment"])]
            comments.extend(("inline", comment) for comment in component["inline_comments"])
            for role, comment in comments:
                chars = known_number(comment["chars"], "comment characters")
                if chars > limits.max_comment_chars:
                    line = comment["span"]["start_line"] + 1
                    print(f'{report["after"]["path"]}:{line}: {new_name}: '
                          f'{role} comment has {chars} characters, limit {limits.max_comment_chars}')
                    failed = True
    return 1 if failed else 0


def main():
    parser = argparse.ArgumentParser(description="Check functions touched by a cmscout metrics comparison read from stdin.")
    parser.add_argument("--max-complexity", type=int)
    parser.add_argument("--max-increase", type=int)
    parser.add_argument("--max-comment-chars", type=int)
    limits = parser.parse_args()
    values = [limits.max_complexity, limits.max_increase, limits.max_comment_chars]
    if all(value is None for value in values) or any(value is not None and value < 0 for value in values):
        parser.error("choose at least one non-negative limit")
    try:
        return check(json.load(sys.stdin), limits)
    except (ValueError, KeyError, TypeError) as error:
        print(f"invalid metrics report: {error}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
