#!/usr/bin/env python3
"""Use the curated changelog entry without modifying the source changelog."""
import re
import sys
from pathlib import Path


def render(version, changelog):
    match = re.search(r"^## \[" + re.escape(version) + r"\][^\n]*\n(.*?)(?=^## |\Z)", changelog, re.M | re.S)
    if not match or not match.group(1).strip():
        raise ValueError(f"missing changelog entry for {version}")
    return (f"## SIPREC {version}\n\n"
            "Standalone binary archives are portable CGO-disabled builds: **Opus and G.729 decoding are unavailable**. "
            "Use the Docker image or a native CGO build with libopus and bcg729 for those codecs. "
            "See BUILD-NOTES.md for artifact details.\n\n" + match.group(1).strip() + "\n")


if __name__ == "__main__":
    print(render(sys.argv[1], Path("CHANGELOG.md").read_text()), end="")
