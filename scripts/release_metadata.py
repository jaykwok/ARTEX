#!/usr/bin/env python3
"""Release classification; only the highest stable SemVer tag may promote latest."""
import os
import re
import subprocess

TAG = re.compile(r"v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?")

def classify(tag, tags):
    match = TAG.fullmatch(tag)
    if not match:
        raise ValueError(f"release tag must be vMAJOR.MINOR.PATCH[-prerelease]: {tag}")
    prerelease = match[4] is not None
    if prerelease and any(part.isdigit() and len(part) > 1 and part.startswith("0") for part in match[4].split(".")):
        raise ValueError("numeric prerelease identifiers must not have leading zeroes")
    stable = [tuple(map(int, m.group(1, 2, 3))) for candidate in tags
              if (m := TAG.fullmatch(candidate)) and m[4] is None]
    latest = not prerelease and tuple(map(int, match.group(1, 2, 3))) == max(stable, default=(-1, -1, -1))
    return {"prerelease": str(prerelease).lower(), "latest": str(latest).lower()}

if __name__ == "__main__":
    tags = subprocess.check_output(["git", "tag", "--list"], text=True).splitlines()
    result = classify(os.environ["GITHUB_REF_NAME"], tags)
    with open(os.environ["GITHUB_OUTPUT"], "a", encoding="utf-8") as output:
        for key, value in result.items():
            output.write(f"{key}={value}\n")
