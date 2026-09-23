"""Regression tests for the differential review gate."""

import contextlib
import io
import json
import unittest
from pathlib import Path
from unittest.mock import patch

from scripts import conformance_diff


CORPUS = conformance_diff.ROOT / "testdata/conformance"


class DifferentialTest(unittest.TestCase):
    def setUp(self):
        self.manifest = json.loads((CORPUS / "differential.json").read_text())

    def compare_with_reference(self, reference_validate):
        def goskill_outcome(_binary, fixture, _revision):
            if fixture.parent.name == "valid":
                return True, []
            return False, [(fixture / "expected.txt").read_text().strip()]

        with patch.object(
            conformance_diff, "goskill_outcome", side_effect=goskill_outcome
        ):
            with contextlib.redirect_stdout(io.StringIO()):
                return conformance_diff.compare(
                    self.manifest, CORPUS, Path("goskill"), reference_validate
                )

    def test_reviewed_outcomes_pass(self):
        differences = {item["fixture"] for item in self.manifest["differences"]}

        def reference_validate(fixture):
            key = f"{fixture.parent.name}/{fixture.name}"
            return (
                []
                if fixture.parent.name == "valid" or key in differences
                else ["invalid"]
            )

        self.assertEqual(self.compare_with_reference(reference_validate), [])

    def test_stale_difference_fails(self):
        def reference_validate(fixture):
            return [] if fixture.parent.name == "valid" else ["invalid"]

        failures = self.compare_with_reference(reference_validate)
        self.assertTrue(
            any("declared difference changed" in failure for failure in failures)
        )

    def test_new_difference_fails(self):
        differences = {item["fixture"] for item in self.manifest["differences"]}
        differences.add("invalid/missing-name")

        def reference_validate(fixture):
            key = f"{fixture.parent.name}/{fixture.name}"
            return (
                []
                if fixture.parent.name == "valid" or key in differences
                else ["invalid"]
            )

        failures = self.compare_with_reference(reference_validate)
        self.assertIn(
            "invalid/missing-name: unexplained conformance disagreement", failures
        )

    def test_fixture_count_change_fails(self):
        self.manifest["fixture_count"] += 1
        differences = {item["fixture"] for item in self.manifest["differences"]}

        def reference_validate(fixture):
            key = f"{fixture.parent.name}/{fixture.name}"
            return (
                []
                if fixture.parent.name == "valid" or key in differences
                else ["invalid"]
            )

        failures = self.compare_with_reference(reference_validate)
        self.assertTrue(any("fixture count changed" in failure for failure in failures))

    def test_review_record_must_match_pin(self):
        self.manifest["reference"]["reviewed_revision"] = "older revision"
        revision = self.manifest["specification"]["revision"]
        oids = {
            "HEAD": revision,
            "HEAD:docs/specification.mdx": self.manifest["specification"]["source_oid"],
            "HEAD:skills-ref/src/skills_ref": self.manifest["reference"]["source_oid"],
        }

        class Result:
            stdout = revision

        with patch.object(
            conformance_diff, "git_value", side_effect=lambda _, key: oids[key]
        ):
            with patch.object(
                conformance_diff.subprocess, "run", return_value=Result()
            ):
                with self.assertRaisesRegex(ValueError, "needs a review"):
                    conformance_diff.check_pin(
                        self.manifest, Path("checkout"), Path("goskill")
                    )


if __name__ == "__main__":
    unittest.main()
