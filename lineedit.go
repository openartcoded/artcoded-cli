package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"unicode"
	"unicode/utf8"
)

// the line being asked: what is printed before the cursor, redrawn while editing
var currentPrompt string

func printPrompt(p string) {
	currentPrompt = p
	fmt.Fprint(os.Stderr, p)
}

var history []string

func stty(args ...string) (string, error) {
	cmd := exec.Command("stty", args...)
	cmd.Stdin = os.Stdin
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// readLine reads an answer. In a terminal: a line editor (arrows, home / end, delete, ctrl+a/e/u/k/w, history).
func readLine() (string, error) { return readLineMode(false) }

func readLineMode(secret bool) (string, error) {
	if !isWindows() && isTerminal(os.Stdin) {
		if saved, err := stty("-g"); err == nil {
			if _, err := stty("-icanon", "-echo", "-isig", "min", "1", "time", "0"); err == nil {
				defer func() { _, _ = stty(saved) }()
				return editLine(secret)
			}
		}
	}
	// no terminal (pipe, windows): a plain line, cleaned of what keys like the arrows insert
	line, err := stdin.ReadString('\n')
	if err != nil && (err != io.EOF || line == "") {
		if err == io.EOF {
			return "", errCancel
		}
		return "", err
	}
	if secret && isTerminal(os.Stdin) {
		fmt.Fprintln(os.Stderr)
	}
	return cleanInput(strings.TrimRight(line, "\r\n")), nil
}

// cleanInput removes escape sequences (arrows...) and control characters of a line
func cleanInput(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == 0x1b {
			i += size + escapeLength(s[i+size:])
			continue
		}
		if !unicode.IsControl(r) || r == '\t' {
			b.WriteRune(r)
		}
		i += size
	}
	return b.String()
}

// length of an escape sequence after ESC: CSI (ESC [ ... final), SS3 (ESC O x) or ESC x
func escapeLength(s string) int {
	if s == "" {
		return 0
	}
	switch s[0] {
	case '[':
		for i := 1; i < len(s); i++ {
			if s[i] >= 0x40 && s[i] <= 0x7e {
				return i + 1
			}
		}
		return len(s)
	case 'O':
		return min(2, len(s))
	}
	return 1
}

func editLine(secret bool) (string, error) {
	var buf []rune
	pos := 0
	hist := len(history)
	draft := ""
	redraw := func() {
		shown := string(buf)
		if secret {
			shown = strings.Repeat("•", len(buf))
		}
		fmt.Fprintf(os.Stderr, "\r%s%s\x1b[K", currentPrompt, shown)
		if back := len(buf) - pos; back > 0 {
			fmt.Fprintf(os.Stderr, "\x1b[%dD", back)
		}
	}
	setLine := func(s string) {
		buf = []rune(s)
		pos = len(buf)
		redraw()
	}
	for {
		r, _, err := stdin.ReadRune()
		if err != nil {
			fmt.Fprintln(os.Stderr)
			return "", errCancel
		}
		switch r {
		case '\r', '\n':
			fmt.Fprint(os.Stderr, "\r\n")
			line := string(buf)
			if !secret && strings.TrimSpace(line) != "" && (len(history) == 0 || history[len(history)-1] != line) {
				history = append(history, line)
			}
			return line, nil
		case 3: // ctrl+c
			fmt.Fprint(os.Stderr, "^C\r\n")
			return "", errCancel
		case 4: // ctrl+d: delete, or cancel on an empty line
			if len(buf) == 0 {
				fmt.Fprint(os.Stderr, "\r\n")
				return "", errCancel
			}
			if pos < len(buf) {
				buf = append(buf[:pos], buf[pos+1:]...)
			}
		case 127, 8: // backspace
			if pos > 0 {
				buf = append(buf[:pos-1], buf[pos:]...)
				pos--
			}
		case 1: // ctrl+a
			pos = 0
		case 5: // ctrl+e
			pos = len(buf)
		case 2: // ctrl+b
			pos = max(pos-1, 0)
		case 6: // ctrl+f
			pos = min(pos+1, len(buf))
		case 21: // ctrl+u
			buf = append([]rune{}, buf[pos:]...)
			pos = 0
		case 11: // ctrl+k
			buf = buf[:pos]
		case 23: // ctrl+w: the word before the cursor
			start := pos
			for start > 0 && buf[start-1] == ' ' {
				start--
			}
			for start > 0 && buf[start-1] != ' ' {
				start--
			}
			buf = append(buf[:start], buf[pos:]...)
			pos = start
		case 0x1b:
			key := readEscape()
			switch key {
			case "[D", "OD":
				pos = max(pos-1, 0)
			case "[C", "OC":
				pos = min(pos+1, len(buf))
			case "[H", "OH", "[1~", "[7~":
				pos = 0
			case "[F", "OF", "[4~", "[8~":
				pos = len(buf)
			case "[3~":
				if pos < len(buf) {
					buf = append(buf[:pos], buf[pos+1:]...)
				}
			case "[1;5D", "b": // ctrl / alt + left: previous word
				for pos > 0 && buf[pos-1] == ' ' {
					pos--
				}
				for pos > 0 && buf[pos-1] != ' ' {
					pos--
				}
			case "[1;5C", "f":
				for pos < len(buf) && buf[pos] == ' ' {
					pos++
				}
				for pos < len(buf) && buf[pos] != ' ' {
					pos++
				}
			case "[A", "OA":
				if !secret && hist > 0 {
					if hist == len(history) {
						draft = string(buf)
					}
					hist--
					setLine(history[hist])
				}
				continue
			case "[B", "OB":
				if !secret && hist < len(history) {
					hist++
					if hist == len(history) {
						setLine(draft)
					} else {
						setLine(history[hist])
					}
				}
				continue
			case "[200~": // bracketed paste start / end: ignored
			}
		default:
			if r == '\t' {
				r = ' '
			}
			if unicode.IsControl(r) {
				continue
			}
			buf = append(buf[:pos], append([]rune{r}, buf[pos:]...)...)
			pos++
		}
		redraw()
	}
}

// readEscape reads what follows ESC: "[D", "OH", "[3~", "[1;5C"...
func readEscape() string {
	r, _, err := stdin.ReadRune()
	if err != nil {
		return ""
	}
	switch r {
	case '[':
		var b strings.Builder
		b.WriteRune(r)
		for {
			c, _, err := stdin.ReadRune()
			if err != nil {
				return b.String()
			}
			b.WriteRune(c)
			if c >= 0x40 && c <= 0x7e {
				return b.String()
			}
		}
	case 'O':
		c, _, _ := stdin.ReadRune()
		return "O" + string(c)
	}
	return string(r)
}
