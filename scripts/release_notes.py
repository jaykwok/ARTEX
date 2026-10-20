import os
import re
from pathlib import Path


def section(changelog, tag):
    version = tag.removeprefix("v")
    heading = re.search(r"^## \[" + re.escape(version) + r"\].*$", changelog, re.M)
    if not heading:
        raise ValueError(f"missing CHANGELOG entry for {tag}")
    end = re.search(r"^## ", changelog[heading.end():], re.M)
    return changelog[heading.start():heading.end() + end.start() if end else len(changelog)].strip() + "\n"


if __name__ == "__main__":
    Path("release-notes.md").write_text(section(Path("CHANGELOG.md").read_text(encoding="utf-8"),
                                             os.environ["GITHUB_REF_NAME"]), encoding="utf-8")
