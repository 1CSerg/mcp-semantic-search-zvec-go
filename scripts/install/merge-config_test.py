#!/usr/bin/env python3
"""Tests for scripts/install/merge-config.py"""

from __future__ import annotations

import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).resolve().parent / "merge-config.py"


def run_merge(template: Path, dest: Path, *extra: str) -> subprocess.CompletedProcess[str]:
    cmd = [sys.executable, str(SCRIPT), str(template), str(dest), *extra]
    return subprocess.run(cmd, capture_output=True, text=True)


class MergeConfigTest(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)

    def tearDown(self) -> None:
        self.tmp.cleanup()

    def test_fresh_install_creates_from_template(self) -> None:
        template = self.root / "template.yaml"
        dest = self.root / "dest" / "config.yaml"
        template.write_text("active_profile: lmstudio_qwen\n", encoding="utf-8")

        result = run_merge(template, dest)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("created", result.stdout)
        self.assertEqual(dest.read_text(encoding="utf-8"), template.read_text(encoding="utf-8"))

    def test_merge_preserves_active_profile_and_adds_profile(self) -> None:
        template = self.root / "template.yaml"
        dest = self.root / "config.yaml"
        template.write_text(
            """active_profile: lmstudio_qwen
profiles:
  lmstudio_qwen:
    provider: openai_compatible
    dimensions: 1024
  local_multilingual:
    provider: onnx
    dimensions: 384
""",
            encoding="utf-8",
        )
        dest.write_text(
            """active_profile: routerai_bge_m3
profiles:
  routerai_bge_m3:
    provider: openai_compatible
    dimensions: 1024
""",
            encoding="utf-8",
        )

        result = run_merge(template, dest)
        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        self.assertIn("merged", result.stdout)
        text = dest.read_text(encoding="utf-8")
        self.assertIn("active_profile: routerai_bge_m3", text)
        self.assertIn("routerai_bge_m3:", text)
        self.assertIn("local_multilingual:", text)
        self.assertIn("lmstudio_qwen:", text)

    def test_merge_adds_nested_key_without_touching_list(self) -> None:
        template = self.root / "template.yaml"
        dest = self.root / "config.yaml"
        template.write_text(
            """indexing:
  extensions:
    - .go
  heartbeat_seconds: 15
""",
            encoding="utf-8",
        )
        dest.write_text(
            """indexing:
  extensions:
    - .py
    - .md
""",
            encoding="utf-8",
        )

        result = run_merge(template, dest)
        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        text = dest.read_text(encoding="utf-8")
        self.assertIn("- .py", text)
        self.assertIn("- .md", text)
        self.assertIn("heartbeat_seconds: 15", text)
        self.assertNotIn("- .go", text)

    def test_merge_preserves_user_comments(self) -> None:
        template = self.root / "template.yaml"
        dest = self.root / "config.yaml"
        template.write_text(
            """active_profile: lmstudio_qwen
search:
  slow_threshold_seconds: 5
""",
            encoding="utf-8",
        )
        dest.write_text(
            """# my custom note
active_profile: lmstudio_qwen
""",
            encoding="utf-8",
        )

        result = run_merge(template, dest)
        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        text = dest.read_text(encoding="utf-8")
        self.assertIn("# my custom note", text)
        self.assertIn("slow_threshold_seconds: 5", text)

    def test_replace_overwrites_user_config(self) -> None:
        template = self.root / "template.yaml"
        dest = self.root / "config.yaml"
        template.write_text("active_profile: from_template\n", encoding="utf-8")
        dest.write_text("active_profile: user_value\n", encoding="utf-8")

        result = run_merge(template, dest, "--replace")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("replaced", result.stdout)
        self.assertEqual(dest.read_text(encoding="utf-8"), template.read_text(encoding="utf-8"))

    def test_warns_unquoted_description_with_colon(self) -> None:
        template = self.root / "template.yaml"
        dest = self.root / "config.yaml"
        template.write_text(
            """active_profile: test
profiles:
  test:
    description: Foo: Bar
    provider: openai_compatible
    dimensions: 384
""",
            encoding="utf-8",
        )

        result = run_merge(template, dest, "--replace")
        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        self.assertIn("warning:", result.stderr)
        self.assertIn("description contains", result.stderr)

    def test_merge_adds_provider_aware_max_input_tokens(self) -> None:
        template = self.root / "template.yaml"
        dest = self.root / "config.yaml"
        template.write_text(
            """active_profile: local_multilingual
indexing:
  chunking:
    strategy: hybrid
    version: 1
profiles:
  local_multilingual:
    provider: onnx
    dimensions: 384
  lmstudio_qwen:
    provider: openai_compatible
    dimensions: 1024
""",
            encoding="utf-8",
        )
        dest.write_text(
            """active_profile: local_multilingual
profiles:
  local_multilingual:
    provider: onnx
    dimensions: 384
  lmstudio_qwen:
    provider: openai_compatible
    dimensions: 1024
""",
            encoding="utf-8",
        )

        result = run_merge(template, dest)
        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        text = dest.read_text(encoding="utf-8")
        self.assertIn("max_input_tokens: 256", text)
        self.assertIn("max_input_tokens: 512", text)
        self.assertIn("chunking:", text)
        self.assertIn("strategy: hybrid", text)

    def test_merge_preserves_existing_max_input_tokens(self) -> None:
        template = self.root / "template.yaml"
        dest = self.root / "config.yaml"
        template.write_text(
            """profiles:
  local_multilingual:
    provider: onnx
    dimensions: 384
    max_input_tokens: 256
""",
            encoding="utf-8",
        )
        dest.write_text(
            """profiles:
  local_multilingual:
    provider: onnx
    dimensions: 384
    max_input_tokens: 128
""",
            encoding="utf-8",
        )

        result = run_merge(template, dest)
        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        text = dest.read_text(encoding="utf-8")
        self.assertIn("max_input_tokens: 128", text)
        self.assertNotIn("max_input_tokens: 256", text)


if __name__ == "__main__":
    unittest.main()
