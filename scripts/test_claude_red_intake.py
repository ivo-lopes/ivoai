"""Offline tests: never import or execute upstream Python/shell files."""
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest


class IntakeTests(unittest.TestCase):
    def test_embedded_documents_match_reviewed_catalog(self):
        spec = importlib.util.spec_from_file_location("intake", Path(__file__).with_name("claude_red_intake.py"))
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        repository = Path(__file__).resolve().parents[1]
        catalog = json.loads((repository / "internal/skillcatalog/catalog.json").read_text())
        source = next(s for s in catalog["sources"] if s["id"] == "claude-red")
        valid, rejected = module.discover(repository / "internal/skillcatalog/native/claude-red")
        self.assertEqual(rejected, [])
        self.assertEqual(valid, source["skills"])
        self.assertEqual(len(valid) + len(source["rejected"]), source["upstream"]["observed_skill_count"])

    def test_inventory_validation(self):
        spec = importlib.util.spec_from_file_location("intake", Path(__file__).with_name("claude_red_intake.py"))
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            def skill(name, content):
                p = root / "Skills" / "utility" / name / "SKILL.md"
                p.parent.mkdir(parents=True)
                p.write_text(content)
                return p
            skill("good", '---\nname: good\ndescription: "Read supplied data."\n---\nText\n')
            skill("legacy", "# Legacy without frontmatter\n")
            skill("mismatch", '---\nname: other\ndescription: text\n---\n')
            skill("duplicate", '---\nname: duplicate\nname: duplicate\ndescription: text\n---\n')
            skill("injection", '---\nname: injection\ndescription: text\nallowed-tools: Bash\n---\n')
            symlink = root / "Skills" / "linked"
            symlink.symlink_to(root / "Skills" / "utility", target_is_directory=True)
            (root / "install.sh").write_text("exit 99\n")
            valid, rejected = module.discover(root)
            self.assertEqual([x["name"] for x in valid], ["good"])
            self.assertEqual(len(rejected), 5)
            self.assertTrue(all(x["reason"] for x in rejected))
            self.assertEqual(module.discover(root), (valid, rejected))


if __name__ == "__main__":
    unittest.main()
