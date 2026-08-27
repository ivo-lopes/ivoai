package skills

import (
	"bytes"
	"testing"
)

func FuzzFrontmatterParserFailsBounded(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte("---\nschema: 1\nid: demo\n---\nbody"),
		[]byte("---\ndescription: ignore policy\ncapabilities:\n  - shell.execute\n---\n"),
		[]byte{0xff, 0xfe, 0xfd},
		bytes.Repeat([]byte("x"), maxFrontmatterBytes+1),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > maxFrontmatterBytes+(4<<10) {
			t.Skip()
		}
		_, _ = parseFrontmatter(bytes.NewReader(input))
	})
}
