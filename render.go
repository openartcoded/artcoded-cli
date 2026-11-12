package main

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"
)

var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

// runeWidth: the width of s on screen (without the ansi codes; one cell per rune, emoji aside)
func runeWidth(s string) int { return utf8.RuneCountInString(stripANSI(s)) }

func padRight(s string, n int) string {
	if w := runeWidth(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}

func padLeft(s string, n int) string {
	if w := runeWidth(s); w < n {
		return strings.Repeat(" ", n-w) + s
	}
	return s
}

// numeric cells (amounts, hours, percentages) are aligned on the right
var numericRe = regexp.MustCompile(`^[-+]?[\d  .,]+(\s?(€|%|h|d|KB|MB|GB|B))?$|^•+ €$`)

func isNumeric(s string) bool {
	s = strings.TrimSpace(stripANSI(s))
	return s != "" && numericRe.MatchString(s)
}

const maxCell = 60

func cell(c string) string {
	c = strings.ReplaceAll(strings.ReplaceAll(c, "\n", " "), "\t", " ")
	if runeWidth(c) > maxCell {
		return truncate(stripANSI(c), maxCell)
	}
	return c
}

// table prints rows aligned, the numeric columns on the right. Colored cells keep their alignment.
func table(w io.Writer, headers []string, rows [][]string) {
	if len(rows) == 0 {
		fmt.Fprintln(w, dim("(nothing)"))
		return
	}
	cols := len(headers)
	for _, r := range rows {
		cols = max(cols, len(r))
	}
	widths := make([]int, cols)
	numeric := make([]bool, cols)
	cells := make([][]string, len(rows))
	for i := range numeric {
		numeric[i] = true
	}
	for c := 0; c < cols; c++ {
		if c < len(headers) {
			widths[c] = runeWidth(headers[c])
		}
	}
	nonEmpty := make([]int, cols)
	for i, r := range rows {
		cells[i] = make([]string, cols)
		for c := 0; c < cols; c++ {
			if c < len(r) {
				cells[i][c] = cell(r[c])
			}
			v := cells[i][c]
			widths[c] = max(widths[c], runeWidth(v))
			if strings.TrimSpace(stripANSI(v)) != "" {
				nonEmpty[c]++
				if !isNumeric(v) {
					numeric[c] = false
				}
			}
		}
	}
	line := func(values []string, header bool) {
		var b strings.Builder
		for c := 0; c < cols; c++ {
			v := ""
			if c < len(values) {
				v = values[c]
			}
			if header {
				v = strings.ToUpper(v)
			}
			last := c == cols-1
			switch {
			case numeric[c] && nonEmpty[c] > 0:
				b.WriteString(padLeft(v, widths[c]))
			case last:
				b.WriteString(v)
			default:
				b.WriteString(padRight(v, widths[c]))
			}
			if !last {
				b.WriteString("  ")
			}
		}
		out := strings.TrimRight(b.String(), " ")
		if header {
			out = bold(out)
		}
		fmt.Fprintln(w, out)
	}
	hasHeader := false
	for _, h := range headers {
		hasHeader = hasHeader || strings.TrimSpace(h) != ""
	}
	if !hasHeader {
		for _, r := range cells {
			line(r, false)
		}
		return
	}
	line(headers, true)
	total := 0
	for c, wd := range widths {
		total += wd
		if c < cols-1 {
			total += 2
		}
	}
	fmt.Fprintln(w, dim(strings.Repeat("─", min(total, terminalColumns()))))
	for _, r := range cells {
		line(r, false)
	}
}
