package terminalui

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestReadKeyHonorsCancellation(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readKey(ctx, bufio.NewReader(reader), int(reader.Fd())); !errors.Is(err, context.Canceled) {
		t.Fatalf("readKey cancellation error=%v", err)
	}
}

func TestBannerAdaptsToTerminalWidthAndCharacterSet(t *testing.T) {
	wide := Banner(100, false, true)
	medium := Banner(60, false, false)
	narrow := Banner(30, false, true)
	if !strings.Contains(wide, "██") || !strings.Contains(wide, "IVO") && !strings.Contains(wide, "██╗") {
		t.Fatalf("wide banner missing block lettering: %q", wide)
	}
	if !strings.Contains(medium, "___") || strings.Contains(medium, "██") {
		t.Fatalf("medium ASCII banner: %q", medium)
	}
	if strings.TrimSpace(narrow) != "ivoai" {
		t.Fatalf("narrow banner = %q", narrow)
	}
}

func TestRenderSupportsColorAndNoColor(t *testing.T) {
	items := []Item{{ID: "status", Label: "Status", Description: "Readiness"}, {ID: "disabled", Label: "Stop", DisabledReason: "requires root"}}
	badges := []Badge{{Label: "Overall", Value: "READY", Kind: "success"}}
	plain := Render("Dashboard", items, badges, 0, 80, false, true)
	colored := Render("Dashboard", items, badges, 0, 80, true, true)
	if strings.Contains(plain, "\x1b[") || !strings.Contains(plain, "requires root") || !strings.Contains(plain, "READY") {
		t.Fatalf("plain render: %q", plain)
	}
	if !strings.Contains(colored, "\x1b[") {
		t.Fatal("colored render omitted ANSI styling")
	}
}

func TestDecodeNavigationKeys(t *testing.T) {
	tests := map[string]Key{"\x1b[A": KeyUp, "\x1b[B": KeyDown, "j": KeyDown, "k": KeyUp, "\r": KeyEnter, "\x1b": KeyBack, "q": KeyQuit}
	for input, expected := range tests {
		if got := DecodeKey([]byte(input)); got != expected {
			t.Fatalf("DecodeKey(%q)=%v want %v", input, got, expected)
		}
	}
}

func TestPlainSelectorSupportsExitAndRejectsDisabled(t *testing.T) {
	items := []Item{{ID: "ok", Label: "Status"}, {ID: "stop", Label: "Stop", DisabledReason: "requires root"}}
	var output bytes.Buffer
	id, err := (Selector{In: strings.NewReader("0\n"), Out: &output, ForcePlain: true}).Choose("Test", items, nil)
	if err != nil || id != "" {
		t.Fatalf("exit id=%q err=%v", id, err)
	}
	_, err = (Selector{In: strings.NewReader("2\n"), Out: &output, ForcePlain: true}).Choose("Test", items, nil)
	if err == nil || !strings.Contains(err.Error(), "requires root") {
		t.Fatalf("disabled selection error=%v", err)
	}
}
