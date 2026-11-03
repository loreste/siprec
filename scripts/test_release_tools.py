"""Release gates must reject incomplete or altered artifacts before publication."""
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest


def load(name):
    path = Path(__file__).with_name(name + ".py")
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


assets = load("verify-release-assets")
build_info = load("verify-build-info")
notes = load("release-notes")


class ReleaseTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.names = [f"siprec-{target}.tar.gz" for target in ("linux-amd64", "linux-arm64", "darwin-amd64", "darwin-arm64")]
        self.names += ["siprec-windows-amd64.zip", "siprec-server-1.2.3.tgz", "BUILD-NOTES.md"]
        manifest = []
        for name in self.names:
            content = name.encode()
            (self.directory / name).write_bytes(content)
            manifest.append(hashlib.sha256(content).hexdigest() + "  " + name)
        (self.directory / "checksums.txt").write_text("\n".join(manifest) + "\n")

    def test_complete_release(self):
        assets.verify(self.directory, "1.2.3")

    def test_missing_helm_chart(self):
        (self.directory / "siprec-server-1.2.3.tgz").unlink()
        with self.assertRaises(ValueError):
            assets.verify(self.directory, "1.2.3")

    def test_tampered_archive(self):
        (self.directory / self.names[0]).write_bytes(b"changed")
        with self.assertRaisesRegex(ValueError, "checksum mismatch"):
            assets.verify(self.directory, "1.2.3")

    def test_unlisted_and_duplicate_assets(self):
        manifest = self.directory / "checksums.txt"
        lines = manifest.read_text().splitlines()
        for contents in ("\n".join(lines[:-1]), "\n".join(lines + [lines[0]])):
            manifest.write_text(contents)
            with self.assertRaises(ValueError):
                assets.verify(self.directory, "1.2.3")

    def test_unexpected_file(self):
        (self.directory / "old-chart.tgz").write_bytes(b"stale")
        with self.assertRaises(ValueError):
            assets.verify(self.directory, "1.2.3")

    def test_build_identity(self):
        path = self.directory / "info.json"
        path.write_text(json.dumps({"version": "1.2.3", "commit": "expected", "build_time": "2026-11-03T15:00:00Z", "go_version": "go1.26.0", "native_codecs": False}))
        build_info.verify(path, "1.2.3", "expected", False)
        for version, commit, codecs in (("old", "expected", False), ("1.2.3", "old", False), ("1.2.3", "expected", True)):
            with self.assertRaises(ValueError):
                build_info.verify(path, version, commit, codecs)

    def test_curated_changelog_only(self):
        changelog = "## [Unreleased]\nFuture\n## [1.2.3] - today\n### Fixed\nCurrent fix\n## [1.2.2]\nOld fix\n"
        result = notes.render("1.2.3", changelog)
        self.assertIn("Current fix", result)
        self.assertNotIn("Future", result)
        self.assertNotIn("Old fix", result)
        self.assertIn("Opus", result)
        with self.assertRaises(ValueError):
            notes.render("9.9.9", changelog)


if __name__ == "__main__":
    unittest.main()
