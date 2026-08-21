package terminalui

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProgressNonTTYReportsLifecycleAndRedacts(t *testing.T) {
	var output bytes.Buffer
	animated := false
	progress := &Progress{Out: &output, Animate: &animated}
	if err := progress.Run(context.Background(), "Connecting Authorization: Bearer secret-token", func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "secret-token") || !strings.Contains(output.String(), "Connecting") || !strings.Contains(output.String(), "✓") {
		t.Fatalf("unsafe progress output: %q", output.String())
	}
	output.Reset()
	want := errors.New("fixture failure")
	if err := progress.Run(context.Background(), "Updating", func(context.Context) error { return want }); !errors.Is(err, want) {
		t.Fatalf("error=%v", err)
	}
	if !strings.Contains(output.String(), "failed") {
		t.Fatalf("failure output: %q", output.String())
	}
}

func TestProgressReportsCancellation(t *testing.T) {
	var output bytes.Buffer
	animated := false
	progress := &Progress{Out: &output, Animate: &animated}
	err := progress.Run(context.Background(), "Waiting", func(context.Context) error {
		return context.Canceled
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	if !strings.Contains(output.String(), "Waiting cancelled") {
		t.Fatalf("expected cancelled state, got %q", output.String())
	}
}

func TestProgressNonTTYEmitsPeriodicPulse(t *testing.T) {
	var output bytes.Buffer
	animated := false
	progress := &Progress{Out: &output, Animate: &animated, PulseInterval: time.Millisecond}
	if err := progress.Run(context.Background(), "Waiting", func(context.Context) error {
		time.Sleep(4 * time.Millisecond)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "elapsed") {
		t.Fatalf("periodic pulse missing: %q", output.String())
	}
}

func TestLiveWriterSerializesConcurrentOutput(t *testing.T) {
	var output bytes.Buffer
	animated := true
	progress := &Progress{Out: &output, Animate: &animated}
	writer := progress.Writer(&output)
	var group sync.WaitGroup
	for _, value := range []string{"alpha\n", "beta\n", "gamma\n"} {
		group.Add(1)
		go func() {
			defer group.Done()
			_, _ = writer.Write([]byte(value))
		}()
	}
	group.Wait()
	for _, value := range []string{"alpha\n", "beta\n", "gamma\n"} {
		if strings.Count(output.String(), value) != 1 {
			t.Fatalf("concurrent output corrupted: %q", output.String())
		}
	}
}

func TestProgressBarKnownAndUnknownTotals(t *testing.T) {
	progress := &Progress{}
	known := progress.Bar("Download", 50, 100, 10)
	unknown := progress.Bar("Download", 2048, 0, 10)
	if !strings.Contains(known, "50%") || !strings.Contains(known, "[=====") {
		t.Fatalf("known bar: %q", known)
	}
	if !strings.Contains(unknown, "2.0 KiB") || strings.Contains(unknown, "%") {
		t.Fatalf("unknown bar: %q", unknown)
	}
}
