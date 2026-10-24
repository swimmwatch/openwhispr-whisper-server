#!/usr/bin/env python3
"""Select validated CI artifact inputs before release downloads or authentication."""

import argparse
import json
import re
import sys
from pathlib import Path

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from tools.release.validation import require, source

ROOT = Path(__file__).resolve().parents[2]


def select_run(inventory: str, selected_run: str, stage: str, root: Path = ROOT) -> tuple[int, str]:
    require(
        re.fullmatch(r"docs/releases/[A-Za-z0-9._-]+\.json", inventory),
        "Invalid reviewed inventory path",
    )
    require(re.fullmatch(r"[1-9][0-9]*", selected_run), "Invalid CI run identity")
    require(stage in ("candidate", "stable"), "Invalid release stage")

    data = json.loads((root / inventory).read_bytes())
    run_id = int(selected_run)
    require(data["ci_run_refs"][0]["run_id"] == run_id, "Inventory CI run identity mismatch")

    images_run = data.get("candidate_ci_run_id") if stage == "stable" else run_id
    require(
        type(images_run) is int and images_run > 0,
        "Accepted candidate CI run is required",
    )
    image_source = data["candidate_source_sha"]
    source(image_source)

    return images_run, image_source


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--inventory", required=True)
    parser.add_argument("--selected-run", required=True)
    parser.add_argument("--stage", choices=("candidate", "stable"), required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()

    try:
        images_run, image_source = select_run(args.inventory, args.selected_run, args.stage)
        with args.output.open("a", encoding="utf-8") as output:
            output.write(f"images-run={images_run}\nimage-source={image_source}\n")
    except OSError, ValueError, KeyError, TypeError, IndexError:
        print("FAIL: release CI artifact selection is invalid", file=sys.stderr)
        return 1

    return 0


if __name__ == "__main__":
    sys.exit(main())
