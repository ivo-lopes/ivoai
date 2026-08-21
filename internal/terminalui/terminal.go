// Package terminalui provides the dependency-light interactive terminal UI.
package terminalui

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

type Item struct {
	ID             string
	Label          string
	Description    string
	DisabledReason string
}

type Badge struct {
	Label string
	Value string
	Kind  string
}

type Key int

const (
	KeyUnknown Key = iota
	KeyUp
	KeyDown
	KeyEnter
	KeyBack
	KeyQuit
)

type Selector struct {
	Context    context.Context
	In         io.Reader
	Out        io.Writer
	ForcePlain bool
	Width      int
}

func (s Selector) Choose(title string, items []Item, badges []Badge) (string, error) {
	if len(items) == 0 {
		return "", errors.New("menu has no items")
	}
	inFile, inTTY := s.In.(*os.File)
	outFile, outTTY := s.Out.(*os.File)
	interactive := !s.ForcePlain && inTTY && outTTY && term.IsTerminal(int(inFile.Fd())) && term.IsTerminal(int(outFile.Fd())) && os.Getenv("TERM") != "dumb"
	if !interactive {
		return s.choosePlain(title, items, badges)
	}
	width := s.Width
	if width <= 0 {
		if value, _, err := term.GetSize(int(outFile.Fd())); err == nil {
			width = value
		}
	}
	if width <= 0 {
		width = 80
	}
	state, err := term.MakeRaw(int(inFile.Fd()))
	if err != nil {
		return s.choosePlain(title, items, badges)
	}
	defer term.Restore(int(inFile.Fd()), state)
	reader := bufio.NewReader(inFile)
	ctx := s.Context
	if ctx == nil {
		ctx = context.Background()
	}
	selected := firstEnabled(items, 0, 1)
	for {
		_, _ = fmt.Fprint(s.Out, "\x1b[2J\x1b[H", Render(title, items, badges, selected, width, colorEnabled(s.Out), unicodeEnabled()))
		key, err := readKey(ctx, reader, int(inFile.Fd()))
		if err != nil {
			if errors.Is(err, io.EOF) {
				return "", nil
			}
			return "", err
		}
		switch key {
		case KeyUp:
			selected = firstEnabled(items, selected-1, -1)
		case KeyDown:
			selected = firstEnabled(items, selected+1, 1)
		case KeyEnter:
			if items[selected].DisabledReason == "" {
				_, _ = fmt.Fprint(s.Out, "\x1b[2J\x1b[H")
				return items[selected].ID, nil
			}
		case KeyBack, KeyQuit:
			_, _ = fmt.Fprint(s.Out, "\x1b[2J\x1b[H")
			return "", nil
		}
	}
}

func (s Selector) choosePlain(title string, items []Item, badges []Badge) (string, error) {
	width := s.Width
	if width <= 0 {
		width = 80
	}
	_, _ = fmt.Fprint(s.Out, Render(title, nil, badges, -1, width, false, unicodeEnabled()))
	for index, item := range items {
		suffix := ""
		if item.DisabledReason != "" {
			suffix = " [unavailable: " + item.DisabledReason + "]"
		}
		fmt.Fprintf(s.Out, "%d) %s%s\n", index+1, item.Label, suffix)
	}
	_, _ = fmt.Fprint(s.Out, "0) Back / Exit\n> ")
	reader, ok := s.In.(*bufio.Reader)
	if !ok {
		reader = bufio.NewReader(s.In)
	}
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" || line == "0" || strings.EqualFold(line, "q") || strings.EqualFold(line, "exit") {
		return "", nil
	}
	selection, parseErr := strconv.Atoi(line)
	if parseErr != nil || selection < 1 || selection > len(items) {
		return "", errors.New("invalid selection")
	}
	item := items[selection-1]
	if item.DisabledReason != "" {
		return "", fmt.Errorf("%s is unavailable: %s", item.Label, item.DisabledReason)
	}
	return item.ID, nil
}

func Render(title string, items []Item, badges []Badge, selected, width int, color, unicode bool) string {
	var output strings.Builder
	output.WriteString(Banner(width, color, unicode))
	if title != "" {
		fmt.Fprintf(&output, "%s\n", paint(title, violet, color))
	}
	if len(badges) > 0 {
		for index, badge := range badges {
			if index > 0 {
				output.WriteString("  ")
			}
			valueColor := cyan
			switch badge.Kind {
			case "success":
				valueColor = green
			case "warning":
				valueColor = yellow
			case "error":
				valueColor = red
			}
			fmt.Fprintf(&output, "%s %s", paint(badge.Label+":", dim, color), paint(badge.Value, valueColor, color))
		}
		output.WriteString("\n")
	}
	output.WriteString("\n")
	for index, item := range items {
		pointer := "  "
		if index == selected {
			pointer = "> "
		}
		label := item.Label
		if item.DisabledReason != "" {
			label += "  (" + item.DisabledReason + ")"
			label = paint(label, dim, color)
		} else if index == selected {
			label = paint(label, cyan, color)
		}
		fmt.Fprintf(&output, "%s%s\n", paint(pointer, violet, color), label)
		if item.Description != "" && width >= 58 {
			fmt.Fprintf(&output, "    %s\n", paint(item.Description, dim, color))
		}
	}
	if selected >= 0 {
		output.WriteString("\n")
		output.WriteString(paint("↑/↓ navigate  enter select  esc back  q quit", dim, color))
		output.WriteString("\n")
	}
	return output.String()
}

func Banner(width int, color, unicode bool) string {
	if width < 46 {
		return paint("ivoai", cyan, color) + "\n\n"
	}
	if !unicode || width < 72 {
		return paint(` ___ _   _  ___   _  ___
|_ _| | | |/ _ \ / \|_ _|
 | || |_| | (_) / _ \| |
|___|\___/ \___/_/ \_\___|`, cyan, color) + "\n\n"
	}
	lines := []string{
		"██╗██╗   ██╗ ██████╗  █████╗ ██╗",
		"██║██║   ██║██╔═══██╗██╔══██╗██║",
		"██║██║   ██║██║   ██║███████║██║",
		"██║╚██╗ ██╔╝██║   ██║██╔══██║██║",
		"██║ ╚████╔╝ ╚██████╔╝██║  ██║██║",
		"╚═╝  ╚═══╝   ╚═════╝ ╚═╝  ╚═╝╚═╝",
	}
	var output strings.Builder
	for index, line := range lines {
		shade := cyan
		if index == len(lines)-1 {
			shade = violet
		}
		output.WriteString(paint(line, shade, color))
		output.WriteByte('\n')
	}
	output.WriteByte('\n')
	return output.String()
}

func DecodeKey(sequence []byte) Key {
	if len(sequence) == 0 {
		return KeyUnknown
	}
	switch sequence[0] {
	case '\r', '\n':
		return KeyEnter
	case 'q', 'Q':
		return KeyQuit
	case 'k', 'K':
		return KeyUp
	case 'j', 'J':
		return KeyDown
	case 3, 27:
		if len(sequence) >= 3 && sequence[0] == 27 && sequence[1] == '[' {
			if sequence[2] == 'A' {
				return KeyUp
			}
			if sequence[2] == 'B' {
				return KeyDown
			}
		}
		return KeyBack
	}
	return KeyUnknown
}

func readKey(ctx context.Context, reader *bufio.Reader, fd int) (Key, error) {
	for {
		if err := ctx.Err(); err != nil {
			return KeyUnknown, err
		}
		poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		ready, err := unix.Poll(poll, 100)
		if err != nil && !errors.Is(err, unix.EINTR) {
			return KeyUnknown, err
		}
		if ready > 0 {
			break
		}
	}
	first, err := reader.ReadByte()
	if err != nil {
		return KeyUnknown, err
	}
	sequence := []byte{first}
	if first == 27 {
		poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		if ready, _ := unix.Poll(poll, int(EscapeSequenceTimeout/time.Millisecond)); ready > 0 {
			for len(sequence) < 3 && reader.Buffered() > 0 {
				value, readErr := reader.ReadByte()
				if readErr != nil {
					break
				}
				sequence = append(sequence, value)
			}
			if len(sequence) == 1 {
				for len(sequence) < 3 {
					value, readErr := reader.ReadByte()
					if readErr != nil {
						break
					}
					sequence = append(sequence, value)
				}
			}
		}
	}
	return DecodeKey(sequence), nil
}

func firstEnabled(items []Item, start, direction int) int {
	if len(items) == 0 {
		return 0
	}
	if direction == 0 {
		direction = 1
	}
	index := start
	for range len(items) {
		if index < 0 {
			index = len(items) - 1
		}
		if index >= len(items) {
			index = 0
		}
		if items[index].DisabledReason == "" {
			return index
		}
		index += direction
	}
	return 0
}

const (
	reset  = "\x1b[0m"
	cyan   = "\x1b[38;5;81m"
	violet = "\x1b[38;5;141m"
	green  = "\x1b[38;5;77m"
	yellow = "\x1b[38;5;220m"
	red    = "\x1b[38;5;203m"
	dim    = "\x1b[38;5;245m"
)

func paint(value, code string, enabled bool) string {
	if !enabled {
		return value
	}
	return code + value + reset
}

func colorEnabled(out io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	file, ok := out.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

func unicodeEnabled() bool {
	if os.Getenv("IVOAI_ASCII") == "1" {
		return false
	}
	locale := strings.ToUpper(os.Getenv("LC_ALL") + os.Getenv("LC_CTYPE") + os.Getenv("LANG"))
	return locale == "" || strings.Contains(locale, "UTF-8") || strings.Contains(locale, "UTF8")
}

// Pause gives users time to read action output before returning to the menu.
func Pause(in io.Reader, out io.Writer) {
	file, ok := in.(*os.File)
	if !ok || !term.IsTerminal(int(file.Fd())) {
		return
	}
	_, _ = fmt.Fprint(out, "\nPress Enter to return to the menu...")
	_, _ = bufio.NewReader(in).ReadString('\n')
}

// Small delay is exported only through tests using the key decoder; keeping it
// here documents the escape-sequence timeout used by live terminals.
var EscapeSequenceTimeout = 30 * time.Millisecond
