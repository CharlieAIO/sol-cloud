package utils

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

var ErrManualSelection = errors.New("manual selection requested")

type Option struct {
	Key   string
	Label string
}

func String(reader *bufio.Reader, out io.Writer, label, defaultValue string, required bool) (string, error) {
	for {
		if strings.TrimSpace(defaultValue) == "" {
			fmt.Fprintf(out, "%s: ", label)
		} else {
			fmt.Fprintf(out, "%s [%s]: ", label, defaultValue)
		}

		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", err
		}

		value := strings.TrimSpace(line)
		if value == "" {
			value = strings.TrimSpace(defaultValue)
		}
		if required && value == "" {
			fmt.Fprintln(out, "value is required")
			if err == io.EOF {
				return "", io.ErrUnexpectedEOF
			}
			continue
		}
		return value, nil
	}
}

func StringList(reader *bufio.Reader, out io.Writer, title, itemLabel string) ([]string, error) {
	values := make([]string, 0)
	fmt.Fprintf(out, "%s\n", title)
	fmt.Fprintln(out, "Press Enter on an empty line to finish.")

	for {
		value, err := String(reader, out, fmt.Sprintf("%s #%d", itemLabel, len(values)+1), "", false)
		if err != nil {
			return nil, err
		}
		value = strings.TrimSpace(value)
		if value == "" {
			return values, nil
		}
		values = append(values, value)
	}
}

func YesNo(reader *bufio.Reader, out io.Writer, label string, defaultYes bool) (bool, error) {
	defaultLabel := "y/N"
	if defaultYes {
		defaultLabel = "Y/n"
	}
	for {
		fmt.Fprintf(out, "%s [%s]: ", label, defaultLabel)
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return false, err
		}

		answer := strings.ToLower(strings.TrimSpace(line))
		switch answer {
		case "":
			return defaultYes, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			fmt.Fprintln(out, "enter y or n")
			if err == io.EOF {
				return false, io.ErrUnexpectedEOF
			}
		}
	}
}

func Uint64(reader *bufio.Reader, out io.Writer, label string, defaultValue uint64, required bool) (uint64, error) {
	for {
		value, err := String(reader, out, label, strconv.FormatUint(defaultValue, 10), required)
		if err != nil {
			return 0, err
		}
		if value == "" && !required {
			return 0, nil
		}

		parsed, parseErr := strconv.ParseUint(value, 10, 64)
		if parseErr != nil {
			fmt.Fprintln(out, "enter a valid positive integer")
			continue
		}
		return parsed, nil
	}
}

func SelectOptionArrow(in io.Reader, out io.Writer, title string, options []Option, defaultKey string) (string, error) {
	if len(options) == 0 {
		return "", errors.New("no options available for selector")
	}

	inFile, ok := in.(*os.File)
	if !ok {
		return "", errors.New("input is not a terminal file")
	}
	if _, ok := out.(*os.File); !ok {
		return "", errors.New("output is not a terminal file")
	}

	restore, err := enableRawTerminalMode(inFile)
	if err != nil {
		return "", err
	}
	defer restore()
	defer fmt.Fprint(out, "\033[?25h")

	selected := findOptionIndex(options, defaultKey)
	if selected < 0 {
		selected = 0
	}
	search := ""
	filtered := filterOptions(options, search)
	selected = selectedIndexInFiltered(filtered, options[selected].Key)
	if selected < 0 {
		selected = 0
	}
	renderedLines := 0

	for {
		renderedLines = drawOptionMenu(out, title, filtered, selected, search, renderedLines)

		key, text, readErr := readSelectionKey(inFile)
		if readErr != nil {
			clearOptionMenu(out, renderedLines)
			return "", readErr
		}

		switch key {
		case "up":
			selected--
			if selected < 0 {
				selected = len(filtered) - 1
			}
		case "down":
			selected++
			if selected >= len(filtered) {
				selected = 0
			}
		case "text":
			search += text
			filtered = filterOptions(options, search)
			if len(filtered) == 0 {
				filtered = options
				search = ""
			}
			if idx := findOptionMatch(filtered, search); idx >= 0 {
				selected = idx
			} else {
				selected = 0
			}
		case "backspace":
			if len(search) > 0 {
				search = search[:len(search)-1]
			}
			filtered = filterOptions(options, search)
			if len(filtered) == 0 {
				filtered = options
			}
			if selected >= len(filtered) {
				selected = len(filtered) - 1
			}
			if selected < 0 {
				selected = 0
			}
			if idx := findOptionMatch(filtered, search); idx >= 0 {
				selected = idx
			}
		case "enter":
			clearOptionMenu(out, renderedLines)
			return filtered[selected].Key, nil
		case "manual":
			clearOptionMenu(out, renderedLines)
			return "", ErrManualSelection
		case "cancel":
			clearOptionMenu(out, renderedLines)
			return "", errors.New("selection cancelled")
		}
	}
}

func drawOptionMenu(out io.Writer, title string, options []Option, selected int, search string, previousLines int) int {
	if len(options) == 0 {
		return previousLines
	}
	visible := visibleOptionWindow(options, selected, 7)
	lines := len(visible) + 3

	if previousLines > 0 {
		fmt.Fprintf(out, "\r\033[%dA\033[J", previousLines)
	}

	fmt.Fprintf(out, "\033[?25l%s\n", title)
	fmt.Fprintln(out, "Use Up/Down, j/k, type to filter, Enter to select, Ctrl+E for custom, Ctrl+C to cancel")
	if strings.TrimSpace(search) != "" {
		fmt.Fprintf(out, "Filter: %s\n", search)
	} else {
		fmt.Fprintln(out, "Filter:")
	}
	for _, item := range visible {
		cursor := " "
		if item.Index == selected {
			cursor = ">"
		}
		fmt.Fprintf(out, "%s %-22s %s\n", cursor, item.Option.Key, item.Option.Label)
	}
	return lines
}

func clearOptionMenu(out io.Writer, renderedLines int) {
	if renderedLines > 0 {
		fmt.Fprintf(out, "\r\033[%dA\033[J", renderedLines)
	}
	fmt.Fprint(out, "\033[?25h")
}

type visibleOption struct {
	Index  int
	Option Option
}

func visibleOptionWindow(options []Option, selected, limit int) []visibleOption {
	if limit <= 0 || len(options) <= limit {
		items := make([]visibleOption, len(options))
		for i, option := range options {
			items[i] = visibleOption{Index: i, Option: option}
		}
		return items
	}
	start := selected - limit/2
	if start < 0 {
		start = 0
	}
	if start+limit > len(options) {
		start = len(options) - limit
	}
	items := make([]visibleOption, 0, limit)
	for i := start; i < start+limit; i++ {
		items = append(items, visibleOption{Index: i, Option: options[i]})
	}
	return items
}

func filterOptions(options []Option, query string) []Option {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return append([]Option(nil), options...)
	}
	filtered := make([]Option, 0, len(options))
	for _, option := range options {
		key := strings.ToLower(option.Key)
		label := strings.ToLower(option.Label)
		if strings.Contains(key, query) || strings.Contains(label, query) {
			filtered = append(filtered, option)
		}
	}
	return filtered
}

func selectedIndexInFiltered(options []Option, key string) int {
	for i, option := range options {
		if option.Key == key {
			return i
		}
	}
	return -1
}

func findOptionIndex(options []Option, key string) int {
	key = strings.ToLower(strings.TrimSpace(key))
	for i, option := range options {
		if strings.ToLower(option.Key) == key {
			return i
		}
	}
	return -1
}

func findOptionMatch(options []Option, query string) int {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return -1
	}

	for i, option := range options {
		if strings.ToLower(option.Key) == query {
			return i
		}
	}
	for i, option := range options {
		if strings.HasPrefix(strings.ToLower(option.Key), query) {
			return i
		}
	}
	for i, option := range options {
		if strings.Contains(strings.ToLower(option.Label), query) {
			return i
		}
	}
	return -1
}

func readSelectionKey(inFile *os.File) (string, string, error) {
	buf := make([]byte, 3)
	n, err := inFile.Read(buf[:1])
	if err != nil {
		return "", "", err
	}
	if n == 0 {
		return "", "", io.EOF
	}

	switch buf[0] {
	case '\r', '\n':
		return "enter", "", nil
	case 3:
		return "cancel", "", nil
	case 5:
		return "manual", "", nil
	case 127, 8:
		return "backspace", "", nil
	case 27:
		_, err = inFile.Read(buf[1:2])
		if err != nil {
			return "", "", err
		}
		if buf[1] != '[' {
			return "", "", nil
		}
		_, err = inFile.Read(buf[2:3])
		if err != nil {
			return "", "", err
		}
		switch buf[2] {
		case 'A':
			return "up", "", nil
		case 'B':
			return "down", "", nil
		default:
			return "", "", nil
		}
	case 'k', 'K':
		return "up", "", nil
	case 'j', 'J':
		return "down", "", nil
	default:
		ch := strings.ToLower(string(buf[0]))
		if (buf[0] >= 'a' && buf[0] <= 'z') || (buf[0] >= 'A' && buf[0] <= 'Z') || (buf[0] >= '0' && buf[0] <= '9') || buf[0] == '-' {
			return "text", ch, nil
		}
		return "", "", nil
	}
}

func enableRawTerminalMode(inFile *os.File) (func(), error) {
	state, err := sttyGetState(inFile)
	if err != nil {
		return nil, err
	}
	if err := sttySet(inFile, "raw", "-echo"); err != nil {
		return nil, err
	}

	restore := func() {
		_ = sttySet(inFile, state)
	}
	return restore, nil
}

func sttyGetState(inFile *os.File) (string, error) {
	cmd := exec.Command("stty", "-g")
	cmd.Stdin = inFile
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func sttySet(inFile *os.File, args ...string) error {
	cmd := exec.Command("stty", args...)
	cmd.Stdin = inFile
	return cmd.Run()
}
