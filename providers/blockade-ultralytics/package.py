#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
from pathlib import Path


def main():
    parser = argparse.ArgumentParser(description="write an Ultralytics plugin.json")
    parser.add_argument("--name", default="local-ultralytics")
    parser.add_argument("--version", required=True)
    parser.add_argument("--platform", required=True, help="for example darwin-arm64")
    args = parser.parse_args()

    root = Path(__file__).resolve().parent
    paths = [root / "blockade.json", root / "python" / "requirements.txt"]
    paths.extend(
        path
        for path in sorted((root / "python").rglob("*"))
        if path.is_file()
        and path.name != "requirements.txt"
        and "__pycache__" not in path.parts
    )
    files = []
    for path in paths:
        relative = path.relative_to(root).as_posix()
        files.append(
            {
                "path": relative,
                "sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
            }
        )
    manifest = {
        "apiVersion": "provider.plugin/v1alpha1",
        "name": args.name,
        "version": args.version,
        "contracts": [
            {"name": "blockade.provider", "version": "v1alpha1"}
        ],
        "platform": args.platform,
        "files": files,
    }
    output = root / "plugin.json"
    output.write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    os.chmod(output, 0o600)
    print(output)


if __name__ == "__main__":
    main()
