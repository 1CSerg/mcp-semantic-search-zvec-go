#!/usr/bin/env python3
"""Merge repo config.yaml template into target install config (user values win)."""

from __future__ import annotations

import argparse
import re
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

_UNQUOTED_DESCRIPTION_COLON = re.compile(r"^(\s*)description:\s*[^\"'].*:.*", re.MULTILINE)


def warn_unquoted_description_colons(text: str, path: Path) -> None:
    """Warn when description values contain ':' but are not YAML-quoted."""
    for match in _UNQUOTED_DESCRIPTION_COLON.finditer(text):
        line_no = text.count("\n", 0, match.start()) + 1
        print(
            f"warning: {path}:{line_no}: description contains ':' — quote the value in double quotes",
            file=sys.stderr,
        )


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


def _provider_name(profile: CommentedMap) -> str:
    provider = profile.get("provider")
    if provider is None:
        return "openai_compatible"
    return str(provider).strip().lower()


def default_max_input_tokens(provider: str) -> int:
    if provider == "onnx":
        return 256
    return 512


def default_embed_budget_ratio(provider: str) -> float:
    if provider == "onnx":
        return 0.90
    return 0.50


def merge_profile_embed_budget(user: CommentedMap) -> bool:
    """Add provider-aware max_input_tokens / embed_budget_ratio to profiles when missing."""
    profiles = user.get("profiles")
    if not is_mapping(profiles):
        return False
    changed = False
    for _name, profile in profiles.items():
        if not is_mapping(profile):
            continue
        provider = _provider_name(profile)
        if "max_input_tokens" not in profile:
            profile["max_input_tokens"] = default_max_input_tokens(provider)
            changed = True
        if "embed_budget_ratio" not in profile:
            profile["embed_budget_ratio"] = default_embed_budget_ratio(provider)
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
        warn_unquoted_description_colons(args.dest.read_text(encoding="utf-8"), args.dest)
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
    merge_profile_embed_budget(user_data)
    try:
        write_yaml(yaml, args.dest, user_data)
    except OSError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1

    warn_unquoted_description_colons(args.dest.read_text(encoding="utf-8"), args.dest)
    print("merged")
    return 0


if __name__ == "__main__":
    sys.exit(main())
