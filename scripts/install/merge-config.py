#!/usr/bin/env python3
"""Merge repo config.yaml template into target install config (user values win)."""

from __future__ import annotations

import argparse
import shutil
import sys
from copy import deepcopy
from pathlib import Path

try:
    from ruamel.yaml import YAML
    from ruamel.yaml.comments import CommentedMap
except ImportError:
    YAML = None  # type: ignore[misc, assignment]
    CommentedMap = dict  # type: ignore[misc, assignment]


def is_mapping(value: object) -> bool:
    return isinstance(value, CommentedMap)


def merge_missing(template: CommentedMap, user: CommentedMap) -> bool:
    """Add keys from template that are absent in user; recurse into nested maps."""
    changed = False
    for key, template_value in template.items():
        if key not in user:
            user[key] = deepcopy(template_value)
            changed = True
            continue
        user_value = user[key]
        if is_mapping(template_value) and is_mapping(user_value):
            if merge_missing(template_value, user_value):
                changed = True
    return changed


def load_yaml(yaml: YAML, path: Path) -> CommentedMap:
    data = yaml.load(path.read_text(encoding="utf-8"))
    if data is None:
        return CommentedMap()
    if not is_mapping(data):
        raise ValueError(f"{path}: expected YAML mapping at root")
    return data


def write_yaml(yaml: YAML, path: Path, data: CommentedMap) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8", newline="\n") as fh:
        yaml.dump(data, fh)


def main() -> int:
    parser = argparse.ArgumentParser(description="Merge config.yaml template into install target")
    parser.add_argument("template", type=Path, help="Source template config.yaml")
    parser.add_argument("dest", type=Path, help="Target .mcp-semantic-search-zvec-go/config.yaml")
    parser.add_argument(
        "--replace",
        action="store_true",
        help="Replace dest entirely with template (ignore user settings)",
    )
    args = parser.parse_args()

    if not args.template.is_file():
        print(f"error: template not found: {args.template}", file=sys.stderr)
        return 1

    if args.replace or not args.dest.is_file():
        existed = args.dest.is_file()
        args.dest.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(args.template, args.dest)
        print("replaced" if args.replace and existed else "created")
        return 0

    if YAML is None:
        print(
            "skipped (ruamel missing): install ruamel.yaml — "
            "pip install -r scripts/install/requirements.txt",
            file=sys.stderr,
        )
        return 2

    yaml = YAML()
    yaml.preserve_quotes = True
    yaml.default_flow_style = False
    yaml.width = 4096

    try:
        template_data = load_yaml(yaml, args.template)
        user_data = load_yaml(yaml, args.dest)
    except (OSError, ValueError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1

    merge_missing(template_data, user_data)
    try:
        write_yaml(yaml, args.dest, user_data)
    except OSError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1

    print("merged")
    return 0


if __name__ == "__main__":
    sys.exit(main())
