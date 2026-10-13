"""Tests for KagMCP Release auto-increment and manually chosen Git tags."""
import json
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from release_version import select_version, version_from_tags


class ReleaseVersionTests(unittest.TestCase):
    def test_first_release_is_one_zero(self):
        self.assertEqual(version_from_tags([]), "1.0")
        self.assertEqual(version_from_tags(["experimental", "v0.9-rc1"]), "1.0")

    def test_increment_previous_minor_instead_of_semver_patch(self):
        self.assertEqual(version_from_tags(["1.0"]), "1.1")
        self.assertEqual(version_from_tags(["1.0", "1.1"]), "1.2")
        self.assertEqual(version_from_tags(["1.0", "1.9"]), "1.10")
        self.assertEqual(version_from_tags(["1.0", "1.11"]), "1.12")
        self.assertEqual(version_from_tags(["1.0", "2.5", "1.20"]), "2.6")

    def test_prefixed_old_versions_count_but_new_tag_is_numeric(self):
        self.assertEqual(version_from_tags(["v1.0"]), "1.1")
        self.assertEqual(version_from_tags(["v2.2", "2.3"]), "2.4")

    def test_ignores_non_numeric_custom_tags(self):
        self.assertEqual(
            version_from_tags(["beta-1", "1.0", "v1.2.3", "2.0-rc1"]), "1.1"
        )

    def test_manual_version_is_always_kept(self):
        for manual in ("1.0", "2.5", "beta-1", "v3.0.0", "10.20.30-rc.1"):
            with self.subTest(manual=manual):
                self.assertEqual(
                    select_version(
                        "workflow_dispatch", "refs/heads/main", "main",
                        manual, ["1.0", "1.1"],
                    ),
                    manual,
                )

    def test_blank_manual_version_autoincrements(self):
        self.assertEqual(
            select_version("workflow_dispatch", "refs/heads/main", "main",
                           "", ["1.0", "1.1"]),
            "1.2",
        )

    def test_pushed_tag_is_used_verbatim(self):
        self.assertEqual(
            select_version("push", "refs/tags/1.0", "main", "", ["1.0"]), "1.0"
        )
        self.assertEqual(
            select_version("push", "refs/tags/v1.1", "main", "", ["v1.1"]),
            "v1.1",
        )

    def test_rejects_unsafe_versions_and_wrong_branch(self):
        for name in (
            "../evil", "release 1.0", "-danger", "bad\nTAG=2.0", "x/y",
            "x..y", "x.lock", ".hidden", "a@{b",
        ):
            with self.subTest(name=name):
                with self.assertRaises(ValueError):
                    select_version(
                        "workflow_dispatch", "refs/heads/main", "main",
                        name, [],
                    )
        with self.assertRaises(ValueError):
            select_version("workflow_dispatch", "refs/heads/test", "main",
                           "1.0", [])
        with self.assertRaises(ValueError):
            select_version("push", "refs/heads/main", "main", "", [])
        with self.assertRaises(ValueError):
            select_version("other", "refs/heads/main", "main", "", [])

    def test_command_reads_existing_git_tags(self):
        resolver = Path(__file__).with_name("release_version.py")
        with tempfile.TemporaryDirectory() as folder:
            def git(*arguments):
                return subprocess.run(
                    ["git", "-C", folder, *arguments],
                    check=True, capture_output=True, text=True
                )
            git("init", "-q")
            git("-c", "user.name=CI", "-c", "user.email=ci@example.invalid",
                "commit", "-q", "--allow-empty", "-m", "init")
            for version in ("1.0", "1.9", "beta-1"):
                git("tag", version)
            actual = subprocess.run(
                [sys.executable, str(resolver),
                 "workflow_dispatch", "refs/heads/main", "main", ""],
                cwd=folder, check=True, capture_output=True, text=True
            )
            self.assertEqual(actual.stdout.strip(), "1.10")
            manual = subprocess.run(
                [sys.executable, str(resolver),
                 "workflow_dispatch", "refs/heads/main", "main", "3.5"],
                cwd=folder, check=True, capture_output=True, text=True
            )
            self.assertEqual(manual.stdout.strip(), "3.5")


class GitHubTagReferenceFilterTests(unittest.TestCase):
    # The same jq filter is used by release.yml in the privileged publish job.
    QUERY = (
        'if type != "array" then error("Respuesta inválida de GitHub") '
        'else .[] | select(.ref == $ref) end'
    )

    @unittest.skipUnless(shutil.which("jq"), "jq se prueba en ubuntu-latest")
    def test_empty_and_prefix_refs_do_not_appear_to_exist(self):
        for payload in (
            [],
            [{"ref": "refs/tags/1.10", "object": {"type": "commit", "sha": "a" * 40}}],
        ):
            with self.subTest(payload=payload):
                result = subprocess.run(
                    ["jq", "-c", "--arg", "ref", "refs/tags/1.1", self.QUERY],
                    input=json.dumps(payload),
                    capture_output=True, text=True, check=True,
                )
                self.assertEqual(result.stdout.strip(), "")

    @unittest.skipUnless(shutil.which("jq"), "jq se prueba en ubuntu-latest")
    def test_exact_ref_is_returned_and_error_object_rejected(self):
        ref = {"ref": "refs/tags/1.1", "object": {"type": "commit", "sha": "b" * 40}}
        result = subprocess.run(
            ["jq", "-c", "--arg", "ref", "refs/tags/1.1", self.QUERY],
            input=json.dumps([ref]),
            capture_output=True, text=True, check=True,
        )
        self.assertEqual(json.loads(result.stdout), ref)
        failed = subprocess.run(
            ["jq", "-c", "--arg", "ref", "refs/tags/1.1", self.QUERY],
            input=json.dumps({"message": "Not Found"}),
            capture_output=True, text=True,
        )
        self.assertNotEqual(failed.returncode, 0)


if __name__ == "__main__":
    unittest.main()
