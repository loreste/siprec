#!/usr/bin/env python3
"""Require every release asset and verify the uploaded SHA-256 manifest."""
import hashlib
import sys
from pathlib import Path


def verify(directory, version):
    directory = Path(directory)
    expected = {f"siprec-{target}.tar.gz" for target in ("linux-amd64", "linux-arm64", "darwin-amd64", "darwin-arm64")}
    expected.update({"siprec-windows-amd64.zip", f"siprec-server-{version}.tgz", "BUILD-NOTES.md"})
    actual = {p.name for p in directory.iterdir()}
    if actual != expected | {"checksums.txt"}:
        raise ValueError(f"release asset set mismatch: missing={expected - actual}, unexpected={actual - expected - {'checksums.txt'}}")
    verified = set()
    for line in (directory / "checksums.txt").read_text().splitlines():
        digest, name = line.split(maxsplit=1)
        name = name.removeprefix("*")
        if name not in expected or name in verified:
            raise ValueError(f"unexpected or duplicate manifest entry: {name}")
        path = directory / name
        if not path.is_file() or path.stat().st_size == 0:
            raise ValueError(f"empty or missing asset: {name}")
        with path.open("rb") as stream:
            actual_digest = hashlib.file_digest(stream, "sha256").hexdigest()
        if actual_digest != digest:
            raise ValueError(f"checksum mismatch: {name}")
        verified.add(name)
    if verified != expected:
        raise ValueError(f"unchecked assets: {expected - verified}")


if __name__ == "__main__":
    verify(sys.argv[1], sys.argv[2])
