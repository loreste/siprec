#!/usr/bin/env python3
"""Verify metadata emitted by a freshly built SIPREC executable."""
import datetime
import json
import sys
from pathlib import Path


def verify(path, version, commit, native_codecs):
    info = json.loads(Path(path).read_text())
    for field, expected in (("version", version), ("commit", commit), ("native_codecs", native_codecs)):
        if info.get(field) != expected:
            raise ValueError(f"{field} mismatch: expected {expected!r}, got {info.get(field)!r}")
    if not info.get("go_version", "").startswith("go"):
        raise ValueError("missing Go toolchain version")
    datetime.datetime.strptime(info.get("build_time", ""), "%Y-%m-%dT%H:%M:%SZ")


if __name__ == "__main__":
    verify(sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4] == "true")
