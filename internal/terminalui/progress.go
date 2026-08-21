package terminalui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ivo-lopes/ivoai/internal/platform"
	"golang.org/x/term"
)

type Progress struct {
	Out           io.Writer
	Interval      time.Duration
	PulseInterval time.Duration
	Now           func() time.Time
	Animate       *bool
	ShowHeader    bool
	mu            sync.Mutex
}

type liveWriter struct {
	progress    *Progress
	destination io.Writer
}

func (p *Progress) Run(ctx context.Context, label string, operation func(context.Context) error) error {
	if p == nil || p.Out == nil {
		return operation(ctx)
	}
	label = platform.Redact(strings.TrimSpace(label))
	if label == "" {
		label = "Working"
	}
	start := time.Now()
	if p.Now != nil {
		start = p.Now()
	}
	animated := p.animated()
	color := colorEnabled(p.Out)
	if animated && p.ShowHeader {
		p.write("%s\n\n", Wordmark(color))
	}
	interval := p.Interval
	if interval <= 0 {
		interval = 100 * time.Millisecond
	}
	done := make(chan struct{})
	workerDone := make(chan struct{})
	if animated {
		go func() {
			defer close(workerDone)
			p.animate(done, label, start, interval)
		}()
	} else {
		p.write("... %s\n", label)
		pulseInterval := p.PulseInterval
		if pulseInterval <= 0 {
			pulseInterval = 15 * time.Second
		}
		go func() {
			defer close(workerDone)
			p.pulse(done, label, start, pulseInterval)
		}()
	}
	err := operation(ctx)
	close(done)
	<-workerDone
	if animated {
		p.clearLine()
	}
	elapsed := time.Since(start).Round(time.Millisecond)
	if p.Now != nil {
		elapsed = p.Now().Sub(start).Round(time.Millisecond)
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			p.write("%s %s cancelled (%s)\n", Warning("!", color), label, elapsed)
			return err
		}
		p.write("%s %s failed (%s)\n", Failure("x", color), label, elapsed)
		return err
	}
	p.write("%s %s (%s)\n", Success(successGlyph(), color), label, elapsed)
	return nil
}

func (p *Progress) pulse(done <-chan struct{}, label string, started time.Time, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case now := <-ticker.C:
			p.write("... %s (%s elapsed)\n", label, now.Sub(started).Round(time.Second))
		}
	}
}

func (p *Progress) animate(done <-chan struct{}, label string, started time.Time, interval time.Duration) {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	if !unicodeEnabled() {
		frames = []string{"|", "/", "-", "\\"}
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	index := 0
	for {
		select {
		case <-done:
			return
		case now := <-ticker.C:
			p.write("\r\x1b[2K%s %s  %s", Info(frames[index%len(frames)], colorEnabled(p.Out)), label, now.Sub(started).Round(time.Second))
			index++
		}
	}
}

func successGlyph() string {
	if unicodeEnabled() {
		return "✓"
	}
	return "OK"
}

func (p *Progress) Bar(label string, current, total int64, width int) string {
	if width < 10 {
		width = 10
	}
	if total <= 0 {
		return fmt.Sprintf("%s %s", label, formatBytes(current))
	}
	if current < 0 {
		current = 0
	}
	if current > total {
		current = total
	}
	filled := int(current * int64(width) / total)
	percent := current * 100 / total
	return fmt.Sprintf("%s [%s%s] %3d%% %s/%s", label, strings.Repeat("=", filled), strings.Repeat("-", width-filled), percent, formatBytes(current), formatBytes(total))
}

// Writer serializes operation output with the live status line. It is intended
// for TTY execution only; machine-readable stdout should remain unwrapped.
func (p *Progress) Writer(destination io.Writer) io.Writer {
	if p == nil || destination == nil {
		return destination
	}
	return liveWriter{progress: p, destination: destination}
}

func (w liveWriter) Write(data []byte) (int, error) {
	w.progress.mu.Lock()
	defer w.progress.mu.Unlock()
	if w.progress.animated() {
		_, _ = io.WriteString(w.destination, "\r\x1b[2K")
	}
	return w.destination.Write(data)
}

func (p *Progress) Animated() bool { return p != nil && p.animated() }

func (p *Progress) animated() bool {
	if p.Animate != nil {
		return *p.Animate
	}
	if os.Getenv("CI") != "" || os.Getenv("IVOAI_NO_ANIMATION") == "1" {
		return false
	}
	file, ok := p.Out.(*os.File)
	return ok && term.IsTerminal(int(file.Fd())) && os.Getenv("TERM") != "dumb"
}

func (p *Progress) write(format string, values ...any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, _ = fmt.Fprintf(p.Out, format, values...)
}

func (p *Progress) clearLine() { p.write("\r\x1b[2K") }

func formatBytes(value int64) string {
	const unit = int64(1024)
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	divisor := unit
	exponent := 0
	for amount := value / unit; amount >= unit && exponent < 4; amount /= unit {
		divisor *= unit
		exponent++
	}
	return fmt.Sprintf("%.1f %ciB", float64(value)/float64(divisor), "KMGTPE"[exponent])
}
