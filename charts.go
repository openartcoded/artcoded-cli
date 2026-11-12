package main

import (
	"fmt"
	"math"
	"strings"
)

// Terminal charts made of unicode block and braille characters (every common terminal font has them).

var sparkTicks = []rune("▁▂▃▄▅▆▇█")

// sparkline: one character per value, the height relative to the biggest
func sparkline(values []float64) string {
	maxV := 0.0
	for _, v := range values {
		maxV = max(maxV, v)
	}
	var b strings.Builder
	for _, v := range values {
		switch {
		case v <= 0 || maxV <= 0:
			b.WriteRune(' ')
		default:
			i := int(math.Round(v / maxV * float64(len(sparkTicks)-1)))
			b.WriteRune(sparkTicks[i])
		}
	}
	return b.String()
}

// axisLabel: a short amount for an axis (12k, 1.2M)
func axisLabel(v float64, unit string) string {
	if demoMode && unit == "€" {
		return "•••"
	}
	a := math.Abs(v)
	var s string
	switch {
	case a >= 1e6:
		s = strings.TrimSuffix(fmt.Sprintf("%.1f", v/1e6), ".0") + "M"
	case a >= 1e4:
		s = fmt.Sprintf("%.0fk", v/1e3)
	case a >= 1e3:
		s = strings.TrimSuffix(fmt.Sprintf("%.1f", v/1e3), ".0") + "k"
	default:
		s = strings.TrimSuffix(fmt.Sprintf("%.1f", v), ".0")
	}
	if unit != "" {
		s += " " + unit
	}
	return s
}

// columnChart: vertical bars, eighth blocks on top, labels under them (one or two characters each)
func columnChart(labels []string, values []float64, height int, unit string, color func(string) string) string {
	if bot != nil {
		return chatColumns(labels, values, unit)
	}
	maxV := 0.0
	for _, v := range values {
		maxV = max(maxV, v)
	}
	if maxV <= 0 {
		return dim("(nothing)") + "\n"
	}
	colW := 2
	for _, l := range labels {
		colW = max(colW, runeWidth(l)+1)
	}
	// fit the terminal
	axisW := max(runeWidth(axisLabel(maxV, unit)), runeWidth(axisLabel(maxV/2, unit))) + 1
	for colW > 2 && axisW+colW*len(values) > terminalColumns()-2 {
		colW--
	}
	barW := max(1, colW-1)
	var b strings.Builder
	for row := height; row >= 1; row-- {
		switch row {
		case height:
			b.WriteString(dim(padLeft(axisLabel(maxV, unit), axisW-1)) + " ")
		case (height + 1) / 2:
			b.WriteString(dim(padLeft(axisLabel(maxV/2, unit), axisW-1)) + " ")
		default:
			b.WriteString(strings.Repeat(" ", axisW))
		}
		for _, v := range values {
			eighths := int(math.Round(v / maxV * float64(height*8)))
			full := (row - 1) * 8
			var c string
			switch {
			case eighths >= full+8:
				c = "█"
			case eighths > full:
				c = string(sparkTicks[eighths-full-1])
			default:
				c = " "
			}
			b.WriteString(color(strings.Repeat(c, barW)) + strings.Repeat(" ", colW-barW))
		}
		b.WriteString("\n")
	}
	b.WriteString(strings.Repeat(" ", axisW))
	for _, l := range labels {
		b.WriteString(dim(padRight(truncate(l, colW), colW)))
	}
	b.WriteString("\n")
	return b.String()
}

// Series of a line chart; NaN: no point (e.g. the months to come)
type Series struct {
	Name   string
	Values []float64
	Color  func(string) string
}

var seriesColors = []func(string) string{
	cyan, yellow, green, func(s string) string { return ansi("35", s) }, func(s string) string { return ansi("34", s) }, red,
}

// lineChart draws series on a braille canvas: every character holds 2 x 4 dots.
// x: the index of the values (spread over the width), y: from 0 to the biggest value.
func lineChart(series []Series, labels []string, width, height int, unit string) string {
	if bot != nil {
		return chatLines(series, labels, unit)
	}
	n, maxV := 0, 0.0
	for _, s := range series {
		n = max(n, len(s.Values))
		for _, v := range s.Values {
			if !math.IsNaN(v) {
				maxV = max(maxV, v)
			}
		}
	}
	if n == 0 || maxV <= 0 {
		return dim("(nothing)") + "\n"
	}
	axisW := runeWidth(axisLabel(maxV, unit)) + 1
	width = max(10, min(width, terminalColumns()-axisW-2))
	dotsW, dotsH := width*2, height*4
	// per cell: the dots set, and the color of the last series drawn there
	dots := make([][]uint8, height)
	colors := make([][]int, height)
	for r := range dots {
		dots[r] = make([]uint8, width)
		colors[r] = make([]int, width)
		for c := range colors[r] {
			colors[r][c] = -1
		}
	}
	// braille bits: x 0..1, y 0..3 from the top
	bits := [2][4]uint8{{0x01, 0x02, 0x04, 0x40}, {0x08, 0x10, 0x20, 0x80}}
	set := func(x, y, si int) {
		if x < 0 || y < 0 || x >= dotsW || y >= dotsH {
			return
		}
		r, c := y/4, x/2
		dots[r][c] |= bits[x%2][y%4]
		colors[r][c] = si
	}
	px := func(i int) int {
		if n == 1 {
			return 0
		}
		return int(math.Round(float64(i) / float64(n-1) * float64(dotsW-1)))
	}
	py := func(v float64) int { return dotsH - 1 - int(math.Round(v/maxV*float64(dotsH-1))) }
	for si, s := range series {
		prevX, prevY, has := 0, 0, false
		for i, v := range s.Values {
			if math.IsNaN(v) {
				has = false
				continue
			}
			x, y := px(i), py(v)
			if has {
				// a line between the points (Bresenham)
				dx, dy := abs(x-prevX), -abs(y-prevY)
				sx, sy := sign(x-prevX), sign(y-prevY)
				err, cx, cy := dx+dy, prevX, prevY
				for {
					set(cx, cy, si)
					if cx == x && cy == y {
						break
					}
					e2 := 2 * err
					if e2 >= dy {
						err += dy
						cx += sx
					}
					if e2 <= dx {
						err += dx
						cy += sy
					}
				}
			} else {
				set(x, y, si)
			}
			prevX, prevY, has = x, y, true
		}
	}
	var b strings.Builder
	for r := 0; r < height; r++ {
		switch r {
		case 0:
			b.WriteString(dim(padLeft(axisLabel(maxV, unit), axisW-1)) + "┤")
		case height / 2:
			b.WriteString(dim(padLeft(axisLabel(maxV/2, unit), axisW-1)) + "┤")
		default:
			b.WriteString(strings.Repeat(" ", axisW-1) + dim("│"))
		}
		for c := 0; c < width; c++ {
			if dots[r][c] == 0 {
				b.WriteRune(' ')
				continue
			}
			ch := string(rune(0x2800 + int(dots[r][c])))
			color := series[colors[r][c]].Color
			if color == nil {
				color = seriesColors[colors[r][c]%len(seriesColors)]
			}
			b.WriteString(color(ch))
		}
		b.WriteString("\n")
	}
	b.WriteString(strings.Repeat(" ", axisW-1) + dim("└"+strings.Repeat("─", width)) + "\n")
	if len(labels) > 0 {
		// the labels under their x, when they fit
		line := []rune(strings.Repeat(" ", width+2))
		next := 0
		for i, l := range labels {
			x := px(i) / 2
			if x < next || x+runeWidth(l) > width+1 {
				continue
			}
			copy(line[x:], []rune(l))
			next = x + runeWidth(l) + 1
		}
		b.WriteString(strings.Repeat(" ", axisW) + dim(strings.TrimRight(string(line), " ")) + "\n")
	}
	var legend []string
	for si, s := range series {
		color := s.Color
		if color == nil {
			color = seriesColors[si%len(seriesColors)]
		}
		legend = append(legend, color("━━")+" "+s.Name)
	}
	b.WriteString(strings.Repeat(" ", axisW) + strings.Join(legend, "   ") + "\n")
	return b.String()
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func sign(v int) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}
