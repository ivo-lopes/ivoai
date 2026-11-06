package workers

import (
	"context"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestWorkerCaptureBoundsWriterFastPaths(t *testing.T) {
	payload := strings.Repeat("worker evidence\n", 50)
	for name, write := range map[string]func(io.Writer) (int64, error){
		"write": func(w io.Writer) (int64, error) {
			n, err := w.Write([]byte(payload))
			return int64(n), err
		},
		"string": func(w io.Writer) (int64, error) {
			n, err := io.WriteString(w, payload)
			return int64(n), err
		},
		"copy": func(w io.Writer) (int64, error) {
			return io.Copy(w, struct{ io.Reader }{strings.NewReader(payload)})
		},
	} {
		t.Run(name, func(t *testing.T) {
			out := &limitedBuffer{limit: 128}
			n, err := write(out)
			if err != nil || n != int64(len(payload)) || !out.overflow || out.String() != payload[:128] {
				t.Fatal("standard writer path bypassed worker evidence limit")
			}
		})
	}
}

// This is an ordinary local process test, with no model/provider or credentials.
// It exercises os/exec's real pipe-copy path, rather than a direct buffer call.
func TestWorkerProcessCaptureMarksAndBoundsOverflow(t *testing.T) {
	root := t.TempDir()
	binary := executable(t, root, "output-worker", "#!/bin/sh\nprintf '%"+strconv.Itoa(MaxRawResultBytes+128)+"s' x\n")
	result, err := run(context.Background(), binary, nil, Request{Directory: root, Runtime: filepath.Join(root, "runtime")}, binary, false, nil, []string{"PATH=/usr/bin:/bin"})
	if err == nil || !result.Truncated || len(result.Stdout) != MaxRawResultBytes || result.ExitCode != 0 {
		t.Fatal("actual child pipe escaped capture bounds or lost overflow metadata")
	}
}
