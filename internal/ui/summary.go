package ui

import (
	"fmt"
	"io"
	"strings"
)

// Field is a single label/value row in a command summary.
type Field struct {
	Label string
	Value string
}

// Header prints a compact command header.
func Header(out io.Writer, title string) {
	title = strings.TrimSpace(title)
	if title == "" {
		return
	}
	fmt.Fprintf(out, "\n%s\n%s\n", title, strings.Repeat("-", len(title)))
}

// Fields prints aligned label/value rows.
func Fields(out io.Writer, rows ...Field) {
	width := 0
	for _, row := range rows {
		if strings.TrimSpace(row.Value) == "" {
			continue
		}
		if len(row.Label) > width {
			width = len(row.Label)
		}
	}
	for _, row := range rows {
		value := strings.TrimSpace(row.Value)
		if value == "" {
			continue
		}
		fmt.Fprintf(out, "%-*s  %s\n", width, row.Label, value)
	}
}
