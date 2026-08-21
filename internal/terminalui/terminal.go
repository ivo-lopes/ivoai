// Package terminalui provides the dependency-light interactive terminal UI.
package terminalui

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
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
	KeyResize
)

type Selector struct {
	Context    context.Context
	In         io.Reader
	Out        io.Writer
	ForcePlain bool
	Compact    bool
	Width      int
	Height     int
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
	resize := make(chan os.Signal, 1)
	signal.Notify(resize, syscall.SIGWINCH)
	defer signal.Stop(resize)
	for {
		width, height := s.dimensions(outFile)
		header := BannerSized(width, height, colorEnabled(s.Out), unicodeEnabled())
		if s.Compact {
			header = Wordmark(colorEnabled(s.Out)) + "\n\n"
		}
		_, _ = fmt.Fprint(s.Out, "\x1b[2J\x1b[H", renderSized(title, items, badges, selected, width, height, colorEnabled(s.Out), unicodeEnabled(), header))
		key, err := readKeyEvent(ctx, reader, int(inFile.Fd()), resize)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return "", nil
			}
			return "", err
		}
		switch key {
		case KeyResize:
			continue
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

func (s Selector) dimensions(out *os.File) (int, int) {
	width, height := s.Width, s.Height
	if (width <= 0 || height <= 0) && out != nil {
		if terminalWidth, terminalHeight, err := term.GetSize(int(out.Fd())); err == nil {
			if width <= 0 {
				width = terminalWidth
			}
			if height <= 0 {
				height = terminalHeight
			}
		}
	}
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	if width < 20 {
		width = 20
	}
	if height < 8 {
		height = 8
	}
	return width, height
}

func (s Selector) choosePlain(title string, items []Item, badges []Badge) (string, error) {
	width := s.Width
	if width <= 0 {
		width = 80
	}
	_, _ = fmt.Fprint(s.Out, renderSized(title, nil, badges, -1, width, 24, false, unicodeEnabled(), ""))
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
	return RenderSized(title, items, badges, selected, width, 24, color, unicode)
}

// RenderSized produces a complete screen that fits within the supplied cell
// dimensions. It deliberately works from unstyled text, then applies ANSI, so
// escape sequences never affect wrapping or truncation.
func RenderSized(title string, items []Item, badges []Badge, selected, width, height int, color, unicode bool) string {
	return renderSized(title, items, badges, selected, width, height, color, unicode, BannerSized(width, height, color, unicode))
}

func renderSized(title string, items []Item, badges []Badge, selected, width, height int, color, unicode bool, header string) string {
	if width < 20 {
		width = 20
	}
	if height < 8 {
		height = 8
	}
	var output strings.Builder
	output.WriteString(header)
	if title != "" {
		fmt.Fprintf(&output, "%s\n", paint(truncate(title, width), violet, color))
	}
	if len(badges) > 0 {
		// Very short terminals reserve the scarce vertical space for the
		// selected operation and navigation hints. Keep only the primary health
		// badge instead of allowing wrapped badges to push content below the
		// visible viewport.
		if height < 12 {
			badge := badges[0]
			valueColor := cyan
			switch badge.Kind {
			case "success":
				valueColor = green
			case "warning":
				valueColor = yellow
			case "error":
				valueColor = red
			}
			label := truncate(badge.Label+":", max(1, width-2))
			available := width - displayWidth(label) - 1
			if available > 0 {
				fmt.Fprintf(&output, "%s %s\n", paint(label, dim, color), paint(truncate(badge.Value, available), valueColor, color))
			} else {
				output.WriteString(paint(label, valueColor, color))
				output.WriteByte('\n')
			}
		} else {
			lineWidth := 0
			for _, badge := range badges {
				entry := badge.Label + ": " + badge.Value
				separator := 0
				if lineWidth > 0 {
					separator = 2
				}
				if lineWidth > 0 && lineWidth+separator+displayWidth(entry) > width {
					output.WriteByte('\n')
					lineWidth = 0
					separator = 0
				}
				if separator > 0 {
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
				available := width - lineWidth - separator
				value := truncate(badge.Value, max(1, available-displayWidth(badge.Label)-2))
				fmt.Fprintf(&output, "%s %s", paint(badge.Label+":", dim, color), paint(value, valueColor, color))
				lineWidth += separator + displayWidth(badge.Label) + 2 + displayWidth(value)
			}
			output.WriteString("\n")
		}
	}
	output.WriteString("\n")

	used := strings.Count(output.String(), "\n")
	footerLines := 2
	descriptions := width >= 60 && height >= 22
	rowHeight := 1
	if descriptions {
		rowHeight = 2
	}
	capacity := (height - used - footerLines) / rowHeight
	if capacity < 1 {
		capacity = 1
	}
	start, end := viewport(len(items), selected, capacity)
	for index := start; index < end; index++ {
		item := items[index]
		pointer := "  "
		if index == selected {
			pointer = "> "
		}
		label := item.Label
		if item.DisabledReason != "" {
			label += "  (" + item.DisabledReason + ")"
			label = truncate(label, width-2)
			label = paint(label, dim, color)
		} else if index == selected {
			label = truncate(label, width-2)
			label = paint(label, cyan, color)
		} else {
			label = truncate(label, width-2)
		}
		fmt.Fprintf(&output, "%s%s\n", paint(pointer, violet, color), label)
		if descriptions {
			fmt.Fprintf(&output, "    %s\n", paint(truncate(item.Description, width-4), dim, color))
		}
	}
	if selected >= 0 {
		if start > 0 || end < len(items) {
			position := fmt.Sprintf("Items %d-%d of %d", start+1, end, len(items))
			output.WriteString(paint(truncate(position, width), yellow, color))
			output.WriteByte('\n')
		} else {
			output.WriteString("\n")
		}
		hint := "↑/↓ navigate  enter select  esc back  q quit"
		if width < 60 || !unicode {
			hint = "j/k move  enter select  esc back  q quit"
		}
		output.WriteString(paint(truncate(hint, width), dim, color))
		output.WriteString("\n")
	}
	return output.String()
}

func viewport(total, selected, capacity int) (int, int) {
	if total <= capacity {
		return 0, total
	}
	start := selected - capacity/2
	if start < 0 {
		start = 0
	}
	if start+capacity > total {
		start = total - capacity
	}
	return start, start + capacity
}

func Banner(width int, color, unicode bool) string {
	return BannerSized(width, 24, color, unicode)
}

func BannerSized(width, height int, color, unicode bool) string {
	if width < 46 || height < 14 {
		return paint("ivoai", cyan, color) + "\n\n"
	}
	if !unicode || width < 90 || height < 24 {
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

// Wordmark is the compact header used by command-oriented screens.
func Wordmark(color bool) string { return paint("ivoai", cyan, color) }

func displayWidth(value string) int {
	width := 0
	for _, r := range value {
		if r < 0x20 || (r >= 0x7f && r < 0xa0) {
			continue
		}
		// The project UI uses Latin text and box/block glyphs, all one cell wide.
		// Treat common East Asian ranges as double-width for safe truncation.
		if r >= 0x1100 && (r <= 0x115f || r >= 0x2e80 && r <= 0xa4cf || r >= 0xac00 && r <= 0xd7a3 || r >= 0xf900 && r <= 0xfaff || r >= 0xff01 && r <= 0xff60) {
			width += 2
		} else {
			width++
		}
	}
	return width
}

func truncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if displayWidth(value) <= width {
		return value
	}
	ellipsis := "…"
	if !unicodeEnabled() {
		ellipsis = "..."
	}
	limit := width - displayWidth(ellipsis)
	if limit < 1 {
		return strings.Repeat(".", width)
	}
	var output strings.Builder
	used := 0
	for _, r := range value {
		runeWidth := displayWidth(string(r))
		if used+runeWidth > limit {
			break
		}
		output.WriteRune(r)
		used += runeWidth
	}
	output.WriteString(ellipsis)
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
	return readKeyEvent(ctx, reader, fd, nil)
}

func readKeyEvent(ctx context.Context, reader *bufio.Reader, fd int, resize <-chan os.Signal) (Key, error) {
	for {
		if err := ctx.Err(); err != nil {
			return KeyUnknown, err
		}
		if resize != nil {
			select {
			case <-resize:
				return KeyResize, nil
			default:
			}
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

// Semantic output helpers keep human-facing success and failure states
// consistent without changing machine-readable streams.
func Success(value string, enabled bool) string { return paint(value, green, enabled) }
func Failure(value string, enabled bool) string { return paint(value, red, enabled) }
func Warning(value string, enabled bool) string { return paint(value, yellow, enabled) }
func Info(value string, enabled bool) string    { return paint(value, cyan, enabled) }

func ColorEnabled(out io.Writer) bool { return colorEnabled(out) }

// HumanOutput reports whether out is an interactive terminal suitable for
// screen-oriented presentation. NO_COLOR intentionally does not affect this
// decision: it disables styling, not the compact ivoai wordmark.
func HumanOutput(out io.Writer) bool {
	if os.Getenv("TERM") == "dumb" || os.Getenv("CI") != "" {
		return false
	}
	file, ok := out.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
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
