package main

import (
	"fmt"
	"io"
	"math"
	"strings"
)

// What the commands print, shaped for a chat (WhatsApp: a proportional font, *bold*, _italic_,
// `code`, ``` blocks) instead of a terminal: no aligned columns, a card per row.

// chatStyle: *s* / _s_ when the text does not get in the way of the markers
func chatStyle(s, marker string) string {
	t := strings.TrimSpace(s)
	if t == "" || strings.Contains(t, marker) || strings.Contains(t, "`") {
		return s
	}
	lead := s[:strings.Index(s, t)]
	trail := s[len(lead)+len(t):]
	return lead + marker + t + marker + trail
}

// unstyle: without the *bold* / _italic_ markers around
func unstyle(s string) string {
	s = strings.TrimSpace(s)
	for len(s) >= 2 && (s[0] == '*' || s[0] == '_') && s[len(s)-1] == s[0] {
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	return s
}

// chatMonoWidth: a table up to this width stays a table, in a monospaced block (fits a phone)
const chatMonoWidth = 34

// chatTable: a small table as it is (monospaced), a wide one as a card per row: the first cell
// in bold, then "header: value" lines
func chatTable(w io.Writer, headers []string, rows [][]string) {
	if len(rows) == 0 {
		fmt.Fprintln(w, chatStyle("nothing", "_"))
		return
	}
	hasHeader := false
	for _, h := range headers {
		hasHeader = hasHeader || strings.TrimSpace(h) != ""
	}
	clean := func(s string) string {
		return unstyle(strings.ReplaceAll(stripANSI(s), "\n", " "))
	}
	if hasHeader {
		plain := make([][]string, len(rows))
		for i, r := range rows {
			plain[i] = make([]string, len(r))
			for c, v := range r {
				plain[i][c] = clean(v)
			}
		}
		var mono strings.Builder
		hooks := bot
		bot = nil // the terminal rendering, without the chat markers
		termTable(&mono, headers, plain)
		bot = hooks
		widest := 0
		lines := strings.Split(strings.TrimRight(stripANSI(mono.String()), "\n"), "\n")
		for i, l := range lines {
			if i != 1 { // the line under the headers follows the width of the screen
				widest = max(widest, runeWidth(l))
			}
		}
		if widest <= chatMonoWidth {
			if len(lines) > 1 {
				lines[1] = strings.Repeat("─", widest)
			}
			fmt.Fprint(w, "```\n"+strings.Join(lines, "\n")+"\n```\n")
			return
		}
	}
	var b strings.Builder
	for i, r := range rows {
		if i > 0 && hasHeader {
			b.WriteString("\n")
		}
		// the title: the first cell that has something
		first := -1
		for c := range r {
			if clean(r[c]) != "" {
				first = c
				break
			}
		}
		if first < 0 {
			continue
		}
		if !hasHeader {
			// key / value lists (the figures of the dashboard): one line
			var rest []string
			for _, v := range r[first+1:] {
				if v = clean(v); v != "" {
					rest = append(rest, v)
				}
			}
			line := chatStyle(clean(r[first]), "*")
			if len(rest) > 0 {
				line += ": " + strings.Join(rest, " · ")
			}
			b.WriteString(line + "\n")
			continue
		}
		b.WriteString(chatStyle(clean(r[first]), "*") + "\n")
		for c := first + 1; c < len(r); c++ {
			v := clean(r[c])
			if v == "" {
				continue
			}
			h := ""
			if c < len(headers) {
				h = strings.ToLower(strings.TrimSpace(headers[c]))
			}
			switch {
			case h == "id":
				b.WriteString("id `" + v + "`\n")
			case h == "":
				b.WriteString(v + "\n")
			default:
				b.WriteString(chatStyle(h, "_") + ": " + v + "\n")
			}
		}
	}
	fmt.Fprint(w, b.String())
}

// chatDetails: _key_: value
func chatDetails(w io.Writer, kv ...string) {
	for i := 0; i+1 < len(kv); i += 2 {
		v := strings.TrimSpace(kv[i+1])
		switch {
		case v == "":
		case strings.EqualFold(strings.TrimSpace(kv[i]), "id"):
			fmt.Fprintf(w, "id `%s`\n", unstyle(stripANSI(v)))
		default:
			fmt.Fprintf(w, "%s: %s\n", chatStyle(kv[i], "_"), v)
		}
	}
}

const chatBarWidth = 12

// chatColumns: the columns of a column chart as horizontal bars (a phone is narrow, not short)
func chatColumns(labels []string, values []float64, unit string) string {
	maxV, lw := 0.0, 0
	for i, v := range values {
		maxV = max(maxV, v)
		if i < len(labels) {
			lw = max(lw, runeWidth(labels[i]))
		}
	}
	if maxV <= 0 {
		return chatStyle("nothing", "_") + "\n"
	}
	var b strings.Builder
	b.WriteString("```\n")
	for i, v := range values {
		l := ""
		if i < len(labels) {
			l = labels[i]
		}
		bv := bar(v, maxV, chatBarWidth)
		b.WriteString(padRight(l, lw) + " " + padRight(bv, chatBarWidth) + " " + axisLabel(v, unit) + "\n")
	}
	b.WriteString("```\n")
	return b.String()
}

// chatLines: a line chart as one sparkline per series, with its last value
func chatLines(series []Series, labels []string, unit string) string {
	nw := 0
	for _, s := range series {
		nw = max(nw, runeWidth(s.Name))
	}
	var b strings.Builder
	b.WriteString("```\n")
	if len(labels) > 1 {
		b.WriteString(strings.Repeat(" ", nw+1) + labels[0] + " → " + labels[len(labels)-1] + "\n")
	}
	for _, s := range series {
		var vals []float64
		last := math.NaN()
		for _, v := range s.Values {
			if math.IsNaN(v) {
				continue
			}
			vals = append(vals, v)
			last = v
		}
		line := padRight(s.Name, nw) + " " + sparkline(vals)
		if !math.IsNaN(last) {
			line += " " + axisLabel(last, unit)
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("```\n")
	return b.String()
}
