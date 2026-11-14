"""Offline regression tests for automatic versions, reruns and tag collisions."""
import os
import pathlib
import subprocess
import sys
import tempfile
import unittest

import release_version as release


class ReleaseVersionTest(unittest.TestCase):
    def test_first_stable_release_ignores_prototype_numbers(self):
        for tags in ({}, {"v0.2.10": "old"}, {"v0.999.999": "old", "v1.0.0-rc.1": "preview"}):
            self.assertEqual(release.select_version(tags, "new"), "v1.0.0")

    def test_patch_uses_numeric_order_and_respects_manual_major_minor(self):
        for tags, expected in (({"v1.0.9": "old", "v1.0.10": "old"}, "v1.0.11"),
                               ({"v1.2.3": "old", "v1.1.99": "old"}, "v1.2.4"),
                               ({"v2.0.0": "old"}, "v2.0.1")):
            self.assertEqual(release.select_version(tags, "new"), expected)

    def test_rerun_reuses_its_version(self):
        self.assertEqual(release.select_version({"v1.0.0": "same"}, "same"), "v1.0.0")

    def test_old_rerun_cannot_replace_newer_release(self):
        with self.assertRaisesRegex(ValueError, "newer stable tag"):
            release.select_version({"v1.0.0": "old", "v1.0.1": "new"}, "old")

    def test_manual_version_and_tag_override(self):
        self.assertEqual(release.select_version({"v1.0.0": "old"}, "new", "1.1.0"), "v1.1.0")
        self.assertEqual(release.select_version({}, "new", "v1.0.1", "tag", "v2.0.0"), "v2.0.0")
        self.assertEqual(release.select_version({"v1.0.0": "same"}, "same", "v1.0.0"), "v1.0.0")

    def test_existing_version_cannot_be_reassigned(self):
        with self.assertRaisesRegex(ValueError, "another commit"):
            release.select_version({"v1.0.0": "old"}, "new", "v1.0.0")

    def test_explicit_downgrade_is_rejected(self):
        with self.assertRaisesRegex(ValueError, "older than"):
            release.select_version({"v1.1.0": "old"}, "new", "v1.0.1")

    def test_invalid_or_prototype_versions_are_rejected(self):
        for version in ("v0.2.99", "v1.0", "v01.0.0", "v1.0.0-rc.1", "v1.0.0+build", "v1.0.0\nother", "--exec=bad"):
            with self.subTest(version=version), self.assertRaises(ValueError):
                release.select_version({}, "new", version)

    def test_manual_feature_branch_is_not_production(self):
        with self.assertRaisesRegex(ValueError, "main"):
            release.select_version({}, "new", "v1.0.0", "branch", "dev")

    def test_cli_with_lightweight_and_annotated_tags(self):
        script = pathlib.Path(release.__file__).resolve()
        with tempfile.TemporaryDirectory(prefix="ytaudio-version-test-") as directory:
            directory = pathlib.Path(directory)
            def git(*args):
                subprocess.run(["git", "-c", "user.name=Release test", "-c", "user.email=release@example.invalid", *args],
                               cwd=directory, check=True, capture_output=True)
            git("init", "--initial-branch=main")
            git("commit", "--allow-empty", "-m", "fixture")
            git("update-ref", "refs/remotes/origin/main", "HEAD")
            git("tag", "v0.2.10")
            output = directory / "output"
            env = dict(os.environ, INPUT_VERSION="", REF_TYPE="branch", REF_NAME="main", GITHUB_OUTPUT=str(output))
            def select(expected):
                result = subprocess.run([sys.executable, str(script)], cwd=directory, env=env, check=True, capture_output=True, text=True)
                self.assertEqual(result.stdout.strip(), expected)
                self.assertEqual(output.read_text().splitlines()[-1], f"version={expected}")
            select("v1.0.0")
            git("tag", "-a", "v1.0.0", "-m", "fixture release")
            select("v1.0.0")
            git("commit", "--allow-empty", "-m", "next fixture")
            git("update-ref", "refs/remotes/origin/main", "HEAD")
            select("v1.0.1")
            env["INPUT_VERSION"] = "v1.1.0"
            select("v1.1.0")
            # An old failed workflow must not publish old code with a new number.
            git("checkout", "--detach", "v1.0.0")
            previous_output = output.read_text()
            result = subprocess.run([sys.executable, str(script)], cwd=directory, env=env, capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("outdated run", result.stderr)
            self.assertEqual(output.read_text(), previous_output)


if __name__ == "__main__":
    unittest.main()
