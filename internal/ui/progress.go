package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// Progress renders a compact task progress line for interactive terminals and
// plain append-only status lines for redirected output.
type Progress struct {
	out         io.Writer
	total       int
	width       int
	interactive bool
	frames      []string

	mu      sync.Mutex
	started bool
	done    bool
	current int
	frame   int
	message string
	start   time.Time
	stop    chan struct{}
	once    sync.Once
}

// NewProgress creates a progress renderer. A total <= 0 uses an indeterminate
// bar, but callers should pass a total when the workflow has known stages.
func NewProgress(out io.Writer, total int) *Progress {
	if out == nil {
		out = io.Discard
	}
	return &Progress{
		out:         out,
		total:       total,
		width:       24,
		interactive: IsTerminal(out),
		frames:      []string{"-", "\\", "|", "/"},
		stop:        make(chan struct{}),
	}
}

// Start begins rendering progress with the supplied message.
func (p *Progress) Start(message string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	if p.started {
		p.message = message
		p.mu.Unlock()
		return
	}
	p.started = true
	p.start = time.Now()
	p.message = message
	p.mu.Unlock()

	if !p.interactive {
		p.printLine("run", message)
		return
	}

	p.render()
	go p.loop()
}

// Step advances the progress bar by one stage and updates the message.
func (p *Progress) Step(message string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	if !p.started {
		p.started = true
		p.start = time.Now()
	}
	if p.total <= 0 || p.current < p.total {
		p.current++
	}
	p.message = message
	p.mu.Unlock()

	if p.interactive {
		p.render()
		return
	}
	p.printLine("run", message)
}

// Detail updates the current message without advancing the bar.
func (p *Progress) Detail(message string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	if !p.started {
		p.started = true
		p.start = time.Now()
	}
	p.message = message
	p.mu.Unlock()

	if p.interactive {
		p.render()
		return
	}
	p.printLine("info", message)
}

// Success completes progress with a success line.
func (p *Progress) Success(message string) {
	p.finish("done", message)
}

// Fail completes progress with a failure line.
func (p *Progress) Fail(message string) {
	p.finish("fail", message)
}

func (p *Progress) finish(status, message string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	if !p.started {
		p.started = true
		p.start = time.Now()
	}
	if p.total > 0 && status == "done" {
		p.current = p.total
	}
	p.message = message
	p.done = true
	p.mu.Unlock()

	p.once.Do(func() {
		close(p.stop)
	})

	if p.interactive {
		p.renderFinal(status, message)
		return
	}
	p.printLine(status, message)
}

func (p *Progress) loop() {
	ticker := time.NewTicker(120 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			p.mu.Lock()
			if p.done {
				p.mu.Unlock()
				return
			}
			p.frame = (p.frame + 1) % len(p.frames)
			p.mu.Unlock()
			p.render()
		case <-p.stop:
			return
		}
	}
}

func (p *Progress) render() {
	p.mu.Lock()
	line := p.lineLocked("run", p.message)
	p.mu.Unlock()
	fmt.Fprintf(p.out, "\r\033[2K%s", line)
}

func (p *Progress) renderFinal(status, message string) {
	p.mu.Lock()
	line := p.lineLocked(status, message)
	p.mu.Unlock()
	fmt.Fprintf(p.out, "\r\033[2K%s\n", line)
}

func (p *Progress) lineLocked(status, message string) string {
	elapsed := time.Since(p.start).Round(time.Second)
	if elapsed < time.Second {
		elapsed = 0
	}

	prefix := status
	if status == "run" {
		prefix = p.frames[p.frame]
	}

	count := ""
	if p.total > 0 {
		count = fmt.Sprintf(" %d/%d", p.current, p.total)
	}

	return fmt.Sprintf("%s %s%s %s %s", prefix, p.barLocked(), count, strings.TrimSpace(message), elapsed)
}

func (p *Progress) barLocked() string {
	width := p.width
	if width <= 0 {
		width = 24
	}
	if p.total <= 0 {
		pos := p.frame % width
		var b strings.Builder
		b.WriteByte('[')
		for i := 0; i < width; i++ {
			if i == pos {
				b.WriteByte('=')
			} else {
				b.WriteByte('.')
			}
		}
		b.WriteByte(']')
		return b.String()
	}

	filled := (p.current * width) / p.total
	if filled > width {
		filled = width
	}
	var b strings.Builder
	b.WriteByte('[')
	for i := 0; i < width; i++ {
		switch {
		case i < filled:
			b.WriteByte('=')
		case i == filled && p.current < p.total:
			b.WriteByte('>')
		default:
			b.WriteByte('.')
		}
	}
	b.WriteByte(']')
	return b.String()
}

func (p *Progress) printLine(status, message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	fmt.Fprintf(p.out, "%s %s\n", status, message)
}

func IsTerminal(v any) bool {
	file, ok := v.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
