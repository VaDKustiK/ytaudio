#!/usr/bin/env python3
"""Select a stable release version from verified repository tags (build tool)."""
import os
import re
import subprocess
import sys

FIRST_RELEASE = (1, 0, 0)
STABLE_TAG = re.compile(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", re.ASCII)


def parts(tag):
    match = STABLE_TAG.fullmatch(tag)
    return tuple(map(int, match.groups())) if match else None


def select_version(tags, commit, requested="", ref_type="branch", ref_name="main"):
    if ref_type == "branch" and ref_name != "main":
        raise ValueError("Publish branch builds from main; keep feature work on dev.")
    stable = {tag: parts(tag) for tag in tags if parts(tag) is not None}
    latest = max(stable.values(), default=(0, 0, 0))
    explicit = ref_name if ref_type == "tag" else requested.strip()
    if explicit:
        version = explicit if explicit.startswith("v") else "v" + explicit
        number = parts(version)
        if number is None or number < FIRST_RELEASE:
            raise ValueError("Use a stable version at least v1.0.0, such as v1.1.0.")
        if number < latest:
            raise ValueError("This version is older than an existing stable tag.")
        if version in tags and tags[version] != commit:
            raise ValueError("This version already belongs to another commit; choose a newer version.")
        return version

    # Re-running the same commit repairs/retries its release without inventing
    # another version. Reject old reruns that would replace the latest release.
    current = [stable[tag] for tag in stable if tags[tag] == commit and stable[tag] >= FIRST_RELEASE]
    if current:
        number = max(current)
        if number < latest:
            raise ValueError("A newer stable tag exists; do not republish this older commit.")
    else:
        number = FIRST_RELEASE if latest < FIRST_RELEASE else (*latest[:2], latest[2] + 1)
    return "v" + ".".join(map(str, number))


def git(*args):
    return subprocess.check_output(["git", *args], text=True).strip()


def main():
    if git("rev-parse", "--is-shallow-repository") == "true":
        raise ValueError("Release versioning needs full history and tags; use fetch-depth: 0.")
    commit = git("rev-parse", "HEAD")
    ref_type, ref_name = os.getenv("REF_TYPE", "branch"), os.getenv("REF_NAME", "main")
    if ref_type == "branch" and ref_name == "main" and commit != git("rev-parse", "refs/remotes/origin/main"):
        raise ValueError("main has newer commits; release its current tip instead of this outdated run.")
    tags = {tag: git("rev-parse", "--verify", "--end-of-options", tag + "^{commit}")
            for tag in git("tag", "--list", "v*").splitlines() if parts(tag) is not None}
    version = select_version(tags, commit, os.getenv("INPUT_VERSION", ""), ref_type, ref_name)
    print(version)
    if output := os.getenv("GITHUB_OUTPUT"):
        with open(output, "a", encoding="utf-8") as stream:
            stream.write(f"version={version}\n")


if __name__ == "__main__":
    try:
        main()
    except (ValueError, subprocess.CalledProcessError) as error:
        print(f"Release version selection failed: {error}", file=sys.stderr)
        sys.exit(1)
