package benchmarkfixtures

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMatrixCoverageAndStructuredOracles(t *testing.T) {
	cases, err := Catalog()
	if err != nil || len(cases) != 16 {
		t.Fatalf("catalog: %v", err)
	}
	supported := 0
	for _, scenario := range cases {
		if !scenario.RunnerSupported {
			continue
		}
		supported++
		decoded, correct := CheckAnswer(scenario.ID, string(scenario.Expected))
		if !decoded || !correct || Schema(scenario.ID) == nil {
			t.Fatalf("missing oracle %s", scenario.ID)
		}
		for _, wrong := range []string{"{}", string(scenario.Expected) + " {}", `{"result":1,"result":1}`, `{"private":"unexpected"}`} {
			if _, correct := CheckAnswer(scenario.ID, wrong); correct {
				t.Fatalf("oracle accepted wrong facts: %s", scenario.ID)
			}
		}
	}
	if supported != 7 {
		t.Fatalf("supported=%d", supported)
	}
	if _, correct := CheckAnswer("small_known_file_correction", `{}`); correct {
		t.Fatal("planned editing scenario passed read-only oracle")
	}
}

func TestMaterializedBytesAndNoncodeMutation(t *testing.T) {
	for _, fixture := range []string{"go-core", "python-deps", "monorepo-ts", "rules-shell", "non-code"} {
		t.Run(fixture, func(t *testing.T) {
			root := t.TempDir()
			if err := Materialize(root, fixture); err != nil {
				t.Fatal(err)
			}
			want, _ := ProjectSHA256(fixture)
			got, err := DirectorySHA256(root)
			if err != nil || got != want {
				t.Fatalf("materialized identity mismatch: %v", err)
			}
			if fixture == "python-deps" {
				if _, err := os.Stat(filepath.Join(root, "pricing/__init__.py")); err != nil {
					t.Fatal("explicit Python package marker was omitted")
				}
			}
			if err := Materialize(root, fixture); err == nil {
				t.Fatal("overwrote existing fixture")
			}
			if fixture == "non-code" {
				if err := os.WriteFile(filepath.Join(root, "milestones.json"), []byte("[]"), 0600); err != nil {
					t.Fatal(err)
				}
				changed, _ := DirectorySHA256(root)
				if changed == want {
					t.Fatal("non-code source mutation not observed")
				}
			}
		})
	}
	root := t.TempDir()
	if Materialize(root, "../go-core") == nil {
		t.Fatal("path traversal admitted")
	}
	if err := os.Symlink("/nonexistent", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := DirectorySHA256(root); err == nil {
		t.Fatal("symlink source admitted")
	}
}

func TestOracleRejectsWrongNumericTypesAndOrder(t *testing.T) {
	for _, text := range []string{`{"order":["review","publish","archive"],"total_days":true}`, `{"order":["archive","publish","review"],"total_days":8}`, `{"order":["review","publish","archive"],"total_days":8,"extra":0}`} {
		if _, correct := CheckAnswer("non_code", text); correct {
			t.Fatal("incorrect non-code answer accepted")
		}
	}
	definition, _ := Lookup("unsupported_parser")
	if _, correct := CheckAnswer("unsupported_parser", strings.Replace(string(definition.Expected), "88", "87", 1)); correct {
		t.Fatal("wrong executable result accepted")
	}
}
