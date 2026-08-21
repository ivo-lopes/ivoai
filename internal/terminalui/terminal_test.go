package terminalui

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
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

func TestHumanOutputRejectsNonTerminalAndCI(t *testing.T) {
	var output bytes.Buffer
	if HumanOutput(&output) {
		t.Fatal("buffer was treated as an interactive terminal")
	}
	t.Setenv("CI", "1")
	if HumanOutput(os.Stdout) {
		t.Fatal("CI output was treated as an interactive screen")
	}
}

func TestRenderSizedNeverExceedsTerminalDimensions(t *testing.T) {
	items := make([]Item, 0, 20)
	for index := 0; index < 20; index++ {
		items = append(items, Item{
			ID:          fmt.Sprintf("item-%d", index),
			Label:       fmt.Sprintf("A deliberately long menu item number %d that must be truncated", index),
			Description: "A description which must not cause the menu to overflow a narrow terminal",
		})
	}
	badges := []Badge{{Label: "Overall", Value: "READY", Kind: "success"}, {Label: "Server", Value: "connected", Kind: "success"}}
	for _, dimensions := range [][2]int{{20, 8}, {35, 10}, {59, 17}, {65, 18}, {100, 24}} {
		width, height := dimensions[0], dimensions[1]
		rendered := RenderSized("Responsive menu", items, badges, 10, width, height, false, true)
		lines := strings.Split(strings.TrimSuffix(rendered, "\n"), "\n")
		if len(lines) > height {
			t.Fatalf("%dx%d render uses %d lines:\n%s", width, height, len(lines), rendered)
		}
		for _, line := range lines {
			if displayWidth(line) > width {
				t.Fatalf("%dx%d line width=%d: %q", width, height, displayWidth(line), line)
			}
		}
		if !strings.Contains(rendered, "of 20") {
			t.Fatalf("%dx%d paginated render lacks position: %q", width, height, rendered)
		}
	}
}

func TestRawTerminalOutputReturnsEveryRowToColumnZero(t *testing.T) {
	got := rawTerminalOutput("first\nsecond\r\nthird\n")
	want := "first\r\nsecond\r\nthird\r\n"
	if got != want {
		t.Fatalf("rawTerminalOutput() = %q, want %q", got, want)
	}
}

func TestPlainFallbackDoesNotAddInteractiveHeader(t *testing.T) {
	var output bytes.Buffer
	_, err := (Selector{In: strings.NewReader("0\n"), Out: &output, ForcePlain: true}).Choose("Dashboard", []Item{{ID: "status", Label: "Status"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "██") || strings.HasPrefix(output.String(), "ivoai\n") {
		t.Fatalf("plain fallback unexpectedly contains lettering: %q", output.String())
	}
}

func TestViewportKeepsSelectionVisible(t *testing.T) {
	for _, selected := range []int{0, 5, 19} {
		start, end := viewport(20, selected, 5)
		if selected < start || selected >= end || end-start != 5 {
			t.Fatalf("selection=%d viewport=%d:%d", selected, start, end)
		}
	}
}

func TestReadKeyEventReportsResize(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	resize := make(chan os.Signal, 1)
	resize <- os.Interrupt
	key, err := readKeyEvent(context.Background(), bufio.NewReader(reader), int(reader.Fd()), resize)
	if err != nil || key != KeyResize {
		t.Fatalf("key=%v err=%v", key, err)
	}
}

func TestReadKeyEventConsumesBufferedArrowSequence(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	if _, err := writer.Write([]byte("\x1b[B")); err != nil {
		t.Fatal(err)
	}
	key, err := readKeyEvent(context.Background(), bufio.NewReader(reader), int(reader.Fd()), nil)
	if err != nil || key != KeyDown {
		t.Fatalf("buffered arrow key=%v err=%v", key, err)
	}
}

func TestCanonicalHeaderIncludesAdaptiveLetteringAndVersion(t *testing.T) {
	for _, dimensions := range [][2]int{{30, 10}, {60, 18}, {100, 30}} {
		header := BannerVersionSized(dimensions[0], dimensions[1], "1.2.3", false, true)
		if !strings.Contains(header, "ivoai") && !strings.Contains(header, "___") && !strings.Contains(header, "██") {
			t.Fatalf("%dx%d header lacks lettering: %q", dimensions[0], dimensions[1], header)
		}
		if !strings.Contains(header, "Version: 1.2.3") {
			t.Fatalf("%dx%d header lacks version: %q", dimensions[0], dimensions[1], header)
		}
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
