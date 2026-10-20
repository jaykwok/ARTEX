import unittest
from pathlib import Path
from release_metadata import classify
from release_notes import section

class ReleaseMetadataTest(unittest.TestCase):
    def test_stable_promotes(self):
        self.assertEqual(classify("v1.1.0", ["v1.0.1", "v1.1.0", "v2.0.0-rc.1"]),
                         {"prerelease": "false", "latest": "true"})
    def test_old_rebuild_does_not_promote(self):
        self.assertEqual(classify("v1.0.1", ["v1.0.1", "v1.1.0"])["latest"], "false")
    def test_prerelease_is_isolated(self):
        self.assertEqual(classify("v1.2.0-rc.1", ["v1.1.0", "v1.2.0-rc.1"]),
                         {"prerelease": "true", "latest": "false"})
    def test_notes_only_include_requested_version(self):
        text = "# History\n## [1.1.0] - today\nNew behavior\n## [1.0.1] - before\nOld behavior\n"
        self.assertIn("New behavior", section(text, "v1.1.0"))
        self.assertNotIn("Old behavior", section(text, "v1.1.0"))
        with self.assertRaises(ValueError):
            section(text, "v2.0.0")

    def test_invalid_prerelease_number_rejected(self):
        with self.assertRaises(ValueError):
            classify("v1.1.0-rc.01", [])

    def test_docker_action_auto_latest_is_disabled(self):
        workflow = (Path(__file__).resolve().parents[1] / ".github/workflows/release.yml").read_text(encoding="utf-8")
        self.assertIn("flavor: latest=false", workflow)
        self.assertIn("type=raw,value=latest,enable=${{ needs.metadata.outputs.latest == 'true' }}", workflow)

    def test_invalid_tag_rejected(self):
        with self.assertRaises(ValueError):
            classify("version-1", [])

if __name__ == "__main__":
    unittest.main()
