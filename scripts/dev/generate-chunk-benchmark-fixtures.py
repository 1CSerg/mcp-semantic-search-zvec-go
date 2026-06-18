#!/usr/bin/env python3
"""Generate synthetic Go/TSX/BSL files for hybrid vs line_window chunk benchmark."""

from __future__ import annotations

import argparse
import random
from pathlib import Path


def write_go(path: Path, idx: int, broken: bool) -> None:
    if broken:
        path.write_text("package broken\n\nfunc {{{() {\n", encoding="utf-8")
        return
    lines = [f"package pkg{idx % 50}\n", "\n"]
    for fn in range(5 + idx % 10):
        lines.append(f"func Func{idx}_{fn}() int {{\n")
        lines.append(f"\treturn {fn}\n")
        lines.append("}\n\n")
    path.write_text("".join(lines), encoding="utf-8")


def write_tsx(path: Path, idx: int, broken: bool) -> None:
    if broken:
        path.write_text("export function Broken() { return <div></ ; }\n", encoding="utf-8")
        return
    lines = [
        "import React from 'react';\n\n",
        f"export interface Props{idx} {{\n  label: string;\n}}\n\n",
        f"export function Component{idx}(props: Props{idx}) {{\n",
        "  return <button>{props.label}</button>;\n",
        "}\n",
    ]
    path.write_text("".join(lines), encoding="utf-8")


def write_bsl(path: Path, idx: int, broken: bool) -> None:
    if broken:
        path.write_text("Процедура Broken(\nКонецПроцедуры\n", encoding="utf-8")
        return
    lines = [
        f"#Область Area{idx}\n\n",
        f"Процедура Proc{idx}() Экспорт\n",
        f"\tСообщить(\"{idx}\");\n",
        "КонецПроцедуры\n\n",
        f"Функция Fn{idx}() Экспорт\n",
        f"\tВозврат {idx};\n",
        "КонецФункции\n\n",
        "#КонецОбласти\n",
    ]
    path.write_text("".join(lines), encoding="utf-8")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("dest", type=Path)
    parser.add_argument("--go", type=int, default=1000)
    parser.add_argument("--tsx", type=int, default=200)
    parser.add_argument("--bsl", type=int, default=200)
    parser.add_argument("--seed", type=int, default=42)
    args = parser.parse_args()
    rng = random.Random(args.seed)
    args.dest.mkdir(parents=True, exist_ok=True)
    for i in range(args.go):
        broken = i < max(1, args.go // 10) and rng.random() < 0.5 or i % 10 == 0
        write_go(args.dest / f"file_{i:04d}.go", i, broken)
    for i in range(args.tsx):
        broken = i < max(1, args.tsx // 10) and rng.random() < 0.5 or i % 10 == 0
        write_tsx(args.dest / f"comp_{i:04d}.tsx", i, broken)
    for i in range(args.bsl):
        broken = i < max(1, args.bsl // 10) and rng.random() < 0.5 or i % 10 == 0
        write_bsl(args.dest / f"mod_{i:04d}.bsl", i, broken)
    print(f"generated {args.go} go + {args.tsx} tsx + {args.bsl} bsl in {args.dest}")


if __name__ == "__main__":
    main()
