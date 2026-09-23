"""Compare every conformance fixture with the reviewed, pinned skills-ref."""

import argparse
import json
import os
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
UPSTREAM = "https://github.com/agentskills/agentskills"
SOURCE_PATHS = {
    "specification": ("docs/specification.mdx", "blob"),
    "reference": ("skills-ref/src/skills_ref", "tree"),
}


def git_value(checkout: Path, expression: str) -> str:
    result = subprocess.run(
        ["git", "-C", str(checkout), "rev-parse", expression],
        check=True,
        capture_output=True,
        text=True,
    )
    return result.stdout.strip()


def check_pin(manifest: dict, checkout: Path, goskill: Path) -> None:
    if manifest.get("schema_version") != "1":
        raise ValueError("unsupported differential manifest version")
    checkout_revision = git_value(checkout, "HEAD")
    goskill_revision = subprocess.run(
        [str(goskill), "spec", "--revision"],
        check=True,
        capture_output=True,
        text=True,
    ).stdout.strip()
    for name, (path, kind) in SOURCE_PATHS.items():
        record = manifest[name]
        revision = record["revision"]
        expected_url = f"{UPSTREAM}/{kind}/{revision}/{path}"
        if revision != checkout_revision or revision != goskill_revision:
            raise ValueError(f"{name} pin differs from checkout or goskill")
        if record["source"] != expected_url:
            raise ValueError(f"{name} source URL is not pinned to its reviewed path")
        if record["source_oid"] != git_value(checkout, f"HEAD:{path}"):
            raise ValueError(
                f"{name} source changed; review and record its new Git object ID"
            )
        if (
            record["reviewed_revision"] != revision
            or not record["review_notes"].strip()
        ):
            raise ValueError(f"{name} needs a review of this revision")
    changed_sources = subprocess.run(
        [
            "git",
            "-C",
            str(checkout),
            "diff",
            "--name-only",
            "HEAD",
            "--",
            "docs/specification.mdx",
            "skills-ref/src/skills_ref",
        ],
        check=True,
        capture_output=True,
        text=True,
    ).stdout.strip()
    if changed_sources:
        raise ValueError(
            f"reviewed upstream sources have local changes: {changed_sources}"
        )


def listed_differences(manifest: dict, corpus: Path) -> dict[str, dict]:
    differences = {}
    for item in manifest["differences"]:
        key = item["fixture"]
        if key in differences:
            raise ValueError(f"duplicate difference: {key}")
        fixture = corpus / key
        if fixture.parent.parent != corpus or not fixture.is_dir():
            raise ValueError(f"unknown difference fixture: {key}")
        if not item["reason"].strip():
            raise ValueError(f"difference needs a reason: {key}")
        if not item["specification_source"].startswith(
            manifest["specification"]["source"] + "#"
        ):
            raise ValueError(f"difference needs a pinned specification section: {key}")
        if (
            type(item["goskill_valid"]) is not bool
            or type(item["reference_valid"]) is not bool
        ):
            raise ValueError(f"difference needs boolean outcomes: {key}")
        if item["goskill_valid"] == item["reference_valid"]:
            raise ValueError(f"listed outcomes agree: {key}")
        if item["goskill_valid"] != (fixture.parent.name == "valid"):
            raise ValueError(f"difference contradicts fixture expectation: {key}")
        expected_file = fixture / "expected.txt"
        if (
            not expected_file.exists()
            or item["rule"] != expected_file.read_text().strip()
        ):
            raise ValueError(f"difference rule does not match fixture: {key}")
        differences[key] = item
    return differences


def goskill_outcome(
    goskill: Path, fixture: Path, revision: str
) -> tuple[bool, list[str]]:
    env = os.environ.copy()
    env["GOSKILL_NO_UPDATE_CHECK"] = "1"
    result = subprocess.run(
        [str(goskill), "validate", "--profile", "spec", "--json", str(fixture)],
        capture_output=True,
        text=True,
        env=env,
        check=False,
    )
    report = json.loads(result.stdout)
    valid = report["valid"]
    if report["profile"] != "spec" or report["specification"]["revision"] != revision:
        raise ValueError(f"wrong goskill profile or specification revision: {fixture}")
    if type(valid) is not bool or result.returncode != (0 if valid else 1):
        raise ValueError(f"goskill result and exit status differ: {fixture}")
    return valid, [diagnostic["code"] for diagnostic in report["diagnostics"]]


def compare(
    manifest: dict, corpus: Path, goskill: Path, reference_validate
) -> list[str]:
    differences = listed_differences(manifest, corpus)
    seen = set()
    failures = []
    agreements = 0
    fixture_count = 0
    print("fixture | goskill | skills-ref | result")
    for kind in ("valid", "invalid"):
        for fixture in sorted((corpus / kind).iterdir()):
            if not fixture.is_dir():
                continue
            fixture_count += 1
            key = f"{kind}/{fixture.name}"
            expected_valid = kind == "valid"
            valid, codes = goskill_outcome(
                goskill, fixture, manifest["specification"]["revision"]
            )
            reference_valid = not reference_validate(fixture)
            if valid != expected_valid:
                failures.append(f"{key}: goskill differs from fixture expectation")
            if not expected_valid and codes != [
                (fixture / "expected.txt").read_text().strip()
            ]:
                failures.append(
                    f"{key}: goskill diagnostic differs from expected.txt: {codes}"
                )
            if expected_valid and codes:
                failures.append(f"{key}: valid fixture has diagnostics: {codes}")
            declared = differences.get(key)
            if declared:
                seen.add(key)
                if (valid, reference_valid) != (
                    declared["goskill_valid"],
                    declared["reference_valid"],
                ):
                    failures.append(
                        f"{key}: declared difference changed or disappeared"
                    )
                result = f"DIFFERENCE {declared['rule']}: {declared['reason']}"
            elif valid != reference_valid:
                failures.append(f"{key}: unexplained conformance disagreement")
                result = "UNEXPLAINED"
            else:
                agreements += 1
                result = "AGREE"
            print(f"{key} | {valid} | {reference_valid} | {result}")
    for key in sorted(differences.keys() - seen):
        failures.append(f"{key}: declared difference was not compared")
    if fixture_count != manifest["fixture_count"]:
        failures.append(
            f"fixture count changed: found {fixture_count}, reviewed {manifest['fixture_count']}"
        )
    print(
        f"Summary: {agreements} agreements, {len(seen)} declared differences, {len(failures)} failures"
    )
    return failures


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--goskill", type=Path, required=True)
    parser.add_argument("--reference-checkout", type=Path, required=True)
    args = parser.parse_args()
    corpus = ROOT / "testdata/conformance"
    manifest = json.loads((corpus / "differential.json").read_text())
    check_pin(manifest, args.reference_checkout, args.goskill.resolve())
    import skills_ref
    from skills_ref import validate

    source_directory = args.reference_checkout.resolve() / "skills-ref/src/skills_ref"
    if Path(skills_ref.__file__).resolve().parent != source_directory:
        raise ValueError("loaded skills_ref does not come from the pinned checkout")
    failures = compare(manifest, corpus, args.goskill.resolve(), validate)
    for failure in failures:
        print(f"FAIL: {failure}", file=sys.stderr)
    return 1 if failures else 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (KeyError, ValueError, OSError, subprocess.CalledProcessError) as exc:
        sys.exit(f"differential check failed: {exc}")
