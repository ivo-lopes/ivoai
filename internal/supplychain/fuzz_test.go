package supplychain

import "testing"

func FuzzArchivePathContainment(f *testing.F) {
	for _, seed := range []string{"SKILL.md", "skills/demo/SKILL.md", "../escape", "/absolute", `..\escape`, "a/../../b", "", "\x00"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 4096 {
			t.Skip()
		}
		value, err := safeArchivePath(input)
		if err == nil && (value == "" || value == "." || value == ".." || value[0] == '/' || len(value) >= 3 && value[:3] == "../") {
			t.Fatalf("unsafe path accepted: input=%q value=%q", input, value)
		}
	})
}
