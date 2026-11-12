package main

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The statistics of the home page of the backoffice, as terminal charts.
func init() {
	register(&Module{
		Name: "stats", Aliases: []string{"statistics", "charts"}, Title: "Statistics", Routes: []string{""},
		Help: "the charts of the home page: per year, month, client, hours, dossiers, expenses",
		Actions: []*Action{
			{Name: "years", Aliases: []string{"overview", "per-year"}, Help: "revenue, days, per day and relative per year", Run: statsYears},
			{Name: "months", Aliases: []string{"per-month"}, Help: "revenue and days invoiced per month", Usage: "[--year 2026 | --all]", Run: statsMonths},
			{Name: "clients", Aliases: []string{"per-client"}, Help: "revenue per client", Usage: "[--year 2026]", Run: statsClients},
			{Name: "hours", Help: "hours worked per year, per client (timesheets)", Run: statsHours},
			{Name: "hours-progress", Aliases: []string{"progress"}, Help: "cumulated hours month by month, one line per year", Usage: "[--client name]", Run: statsHoursProgress},
			{Name: "hours-clients", Help: "hours per client and year, with the progress of this year", Run: statsHoursClients},
			{Name: "dossiers", Help: "earnings and expenses per closed dossier", Run: statsDossiers},
			{Name: "expenses", Help: "expenses per tag", Run: statsExpenses},
		},
	})
}

// ---------- terminal charts ----------

var blocks = []string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}

// bar of value / max on width cells, eighths for the last cell
func bar(value, maxValue float64, width int) string {
	if maxValue <= 0 || value <= 0 || width <= 0 {
		return ""
	}
	eighths := int(math.Round(value / maxValue * float64(width*8)))
	if eighths == 0 {
		eighths = 1
	}
	return strings.Repeat("█", eighths/8) + blocks[eighths%8]
}

func terminalColumns() int {
	if out, err := stty("size"); err == nil {
		var rows, cols int
		if _, err := fmt.Sscanf(out, "%d %d", &rows, &cols); err == nil && cols > 0 {
			return cols
		}
	}
	return 100
}

// barWidth: what is left for the bars after the other columns
func barWidth(used int) int { return max(10, min(60, terminalColumns()-used)) }

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

// runeWidth without the ansi codes
func runeWidth(s string) int {
	n, esc := 0, false
	for _, r := range s {
		switch {
		case r == 0x1b:
			esc = true
		case esc:
			if r == 'm' {
				esc = false
			}
		default:
			n++
		}
	}
	return n
}

func fnum(v float64) string {
	if v == math.Trunc(v) {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	return strconv.FormatFloat(v, 'f', 1, 64)
}

func pctLabel(v float64) string {
	s := fmt.Sprintf("%+.0f%%", v*100)
	if v < 0 {
		return red(s)
	}
	return green(s)
}

// ---------- invoices: like computeInvoiceStats of the backoffice ----------

type yearSummary struct {
	days, revenue float64
	invoices      int
}

type periodStat struct {
	period         string
	earnings, days float64
}

type invoiceStats struct {
	perPeriod     []periodStat
	perClient     map[string]float64
	perYear       map[int]*yearSummary
	total, days   float64
	totalInvoices int
}

// amount of work in days: hours converted with the hours per day of the invoice
func amountInDays(s M) float64 {
	amount := num(s, "amount")
	if amount == 0 {
		return 0
	}
	if str(s, "amountType") == "HOURS" {
		if h := num(s, "hoursPerDay"); h > 0 {
			return amount / h
		}
		return 0
	}
	return amount
}

func loadInvoiceStats(c *Ctx) (*invoiceStats, []M, error) {
	var l []M
	if err := c.api.Post("/api/invoice/find-all-summaries", nil, nil, &l); err != nil {
		return nil, nil, err
	}
	sort.SliceStable(l, func(i, j int) bool {
		a, _ := toTime(get(l[i], "dateOfInvoice"))
		b, _ := toTime(get(l[j], "dateOfInvoice"))
		return a.Before(b)
	})
	st := &invoiceStats{perClient: map[string]float64{}, perYear: map[int]*yearSummary{}}
	idx := map[string]int{}
	for _, s := range l {
		days, sub := amountInDays(s), num(s, "subTotal")
		period := str(s, "period")
		if i, ok := idx[period]; ok {
			st.perPeriod[i].earnings += sub
			st.perPeriod[i].days += days
		} else {
			idx[period] = len(st.perPeriod)
			st.perPeriod = append(st.perPeriod, periodStat{period, sub, days})
		}
		client := defStr(str(s, "client"), "N/A")
		st.perClient[client] += sub
		t, _ := toTime(get(s, "dateOfInvoice"))
		y := st.perYear[t.Year()]
		if y == nil {
			y = &yearSummary{}
			st.perYear[t.Year()] = y
		}
		y.days += days
		y.revenue += sub
		y.invoices++
		st.total += sub
		st.days += days
		st.totalInvoices++
	}
	return st, l, nil
}

func sortedYears[V any](m map[int]V, desc bool) []int {
	ys := make([]int, 0, len(m))
	for y := range m {
		ys = append(ys, y)
	}
	sort.Ints(ys)
	if desc {
		for i, j := 0, len(ys)-1; i < j; i, j = i+1, j-1 {
			ys[i], ys[j] = ys[j], ys[i]
		}
	}
	return ys
}

func loadTimesheetStats(c *Ctx) M {
	var s M
	if err := c.api.Get("/api/timesheet/stats", nil, &s); err != nil {
		return nil // admin only: the columns depending on it are left out
	}
	return s
}

// statsYears: the "Per year" table of the home page, the relative column = revenue / calendar days worked
func statsYears(c *Ctx) error {
	st, raw, err := loadInvoiceStats(c)
	if err != nil {
		return err
	}
	ts := loadTimesheetStats(c)
	worked, _ := get(ts, "workedDays").(map[string]any)
	if c.jsonOut {
		return c.printJSON(M{"invoices": raw, "workedDays": worked})
	}
	years := sortedYears(st.perYear, true)
	if len(years) == 0 {
		fmt.Fprintln(c.out, dim("no invoice yet"))
		return nil
	}
	maxRev := 0.0
	for _, y := range years {
		maxRev = max(maxRev, st.perYear[y].revenue)
	}
	fmt.Fprintf(c.out, "%s  %s\n\n", bold("Per year"), dim(fmt.Sprintf("all time: %s · %s days · %d invoices", money(st.total), fnum(st.days), st.totalInvoices)))
	w := barWidth(95)
	head := padRight("YEAR", 6) + padLeft("REVENUE", 16) + "  " + padRight("", w) + padLeft("DAYS", 8) + padLeft("PER DAY", 14)
	if worked != nil {
		head += padLeft("RELATIVE", 14)
	}
	fmt.Fprintln(c.out, head+padLeft("INVOICES", 10))
	current := time.Now().In(brussels).Year()
	for _, y := range years {
		ys := st.perYear[y]
		perDay := ""
		if ys.days > 0 {
			perDay = money(ys.revenue / ys.days)
		}
		year := strconv.Itoa(y)
		if y == current {
			year = bold(year)
		}
		line := padRight(year, 6) + padLeft(money(ys.revenue), 16) + "  " + padRight(cyan(bar(ys.revenue, maxRev, w)), w) + padLeft(fnum(math.Round(ys.days*10)/10), 8) + padLeft(perDay, 14)
		if worked != nil {
			rel := ""
			if d := num(worked, strconv.Itoa(y)); d > 0 {
				rel = money(ys.revenue / d)
			}
			line += padLeft(rel, 14)
		}
		fmt.Fprintln(c.out, line+padLeft(strconv.Itoa(ys.invoices), 10))
	}
	if worked != nil {
		fmt.Fprintln(c.out, "\n"+dim("relative: revenue / calendar days worked (timesheets), a day counts once; only the years with a timesheet every month, and this one"))
	}
	return nil
}

// periods are MM/YYYY
func periodYear(p string) string {
	if _, y, ok := strings.Cut(p, "/"); ok {
		return y
	}
	return ""
}

func statsMonths(c *Ctx) error {
	st, raw, err := loadInvoiceStats(c)
	if err != nil {
		return err
	}
	year := c.quiet("year", "")
	if year == "" && !c.boolFlag("all") {
		year = strconv.Itoa(time.Now().In(brussels).Year())
		if st.perYear[time.Now().In(brussels).Year()] == nil {
			year = strconv.Itoa(time.Now().In(brussels).Year() - 1)
		}
	}
	var rows []periodStat
	for _, p := range st.perPeriod {
		if year == "" || periodYear(p.period) == year {
			rows = append(rows, p)
		}
	}
	if c.jsonOut {
		out := []M{}
		for _, p := range rows {
			out = append(out, M{"period": p.period, "earnings": p.earnings, "workingDays": p.days})
		}
		return c.printJSON(out)
	}
	_ = raw
	title := "Per month"
	if year != "" {
		title += " " + year
	}
	fmt.Fprintf(c.out, "%s  %s\n\n", bold(title), dim("revenue excl. VAT and days invoiced · other year: --year, every month: --all"))
	if len(rows) == 0 {
		fmt.Fprintln(c.out, dim("no invoice"))
		return nil
	}
	maxE := 0.0
	var total, days float64
	for _, p := range rows {
		maxE = max(maxE, p.earnings)
		total += p.earnings
		days += p.days
	}
	w := barWidth(42)
	for _, p := range rows {
		fmt.Fprintf(c.out, "%s %s %s %s\n", padRight(p.period, 8), padLeft(money(p.earnings), 14), padRight(cyan(bar(p.earnings, maxE, w)), w), dim(fnum(math.Round(p.days*10)/10)+" d"))
	}
	fmt.Fprintf(c.out, "%s %s %s %s\n", padRight("", 8), padLeft(bold(money(total)), 14), padRight("", w), dim(fnum(math.Round(days*10)/10)+" d"))
	return nil
}

func statsClients(c *Ctx) error {
	st, raw, err := loadInvoiceStats(c)
	if err != nil {
		return err
	}
	per := st.perClient
	year := c.quiet("year", "")
	if year != "" {
		per = map[string]float64{}
		for _, s := range raw {
			if t, ok := toTime(get(s, "dateOfInvoice")); ok && strconv.Itoa(t.Year()) == year {
				per[defStr(str(s, "client"), "N/A")] += num(s, "subTotal")
			}
		}
	}
	type kv struct {
		client string
		total  float64
	}
	var rows []kv
	var total, maxT float64
	for k, v := range per {
		rows = append(rows, kv{k, v})
		total += v
		maxT = max(maxT, v)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].total > rows[j].total })
	if c.jsonOut {
		return c.printJSON(per)
	}
	title := "Per client"
	if year != "" {
		title += " " + year
	}
	fmt.Fprintf(c.out, "%s  %s\n\n", bold(title), dim("revenue excl. VAT · a year: --year"))
	nameW := 10
	for _, r := range rows {
		nameW = max(nameW, min(30, runeWidth(r.client)))
	}
	w := barWidth(nameW + 30)
	for _, r := range rows {
		share := ""
		if total > 0 {
			share = fmt.Sprintf("%3.0f%%", r.total/total*100)
		}
		fmt.Fprintf(c.out, "%s %s %s %s\n", padRight(truncate(r.client, 30), nameW), padLeft(money(r.total), 14), padRight(cyan(bar(r.total, maxT, w)), w), dim(share))
	}
	fmt.Fprintf(c.out, "%s %s\n", padRight("", nameW), padLeft(bold(money(total)), 14))
	return nil
}

// ---------- hours: like hours.ts of the backoffice ----------

type tsRow struct {
	year, month int
	client      string
	minutes     float64
}

func timesheetRows(s M) ([]int, []string, []tsRow) {
	var years []int
	for _, y := range list(s, "years") {
		if f, ok := y.(float64); ok {
			years = append(years, int(f))
		}
	}
	sort.Ints(years)
	clients := strList(s, "clients")
	var rows []tsRow
	for _, r := range maps(s["rows"]) {
		rows = append(rows, tsRow{int(num(r, "year")), int(num(r, "month")), str(r, "client"), num(r, "minutes")})
	}
	return years, clients, rows
}

func hoursOf(minutes float64) float64 { return math.Round(minutes/60*10) / 10 }

func needTimesheetStats(c *Ctx) (M, error) {
	var s M
	if err := c.api.Get("/api/timesheet/stats", nil, &s); err != nil {
		return nil, err
	}
	return s, nil
}

// statsHours: hours per year, the 5 most worked clients apart, the rest in "Other"
func statsHours(c *Ctx) error {
	s, err := needTimesheetStats(c)
	if err != nil {
		return err
	}
	if c.jsonOut {
		return c.printJSON(s)
	}
	years, clients, rows := timesheetRows(s)
	top := clients
	if len(top) > 5 {
		top = clients[:5]
	}
	series := append([]string{}, top...)
	if len(clients) > 5 {
		series = append(series, "Other")
	}
	colors := []func(string) string{cyan, green, yellow, func(s string) string { return ansi("35", s) }, func(s string) string { return ansi("34", s) }, dim}
	key := func(cl string) string {
		for _, t := range top {
			if t == cl {
				return cl
			}
		}
		return "Other"
	}
	perYear := map[int]map[string]float64{}
	maxTotal := 0.0
	for _, y := range years {
		perYear[y] = map[string]float64{}
	}
	for _, r := range rows {
		if perYear[r.year] != nil {
			perYear[r.year][key(r.client)] += r.minutes
		}
	}
	for _, y := range years {
		t := 0.0
		for _, v := range perYear[y] {
			t += v
		}
		maxTotal = max(maxTotal, t)
	}
	fmt.Fprintf(c.out, "%s  %s\n\n", bold("Hours per year"), dim("per client (the 5 most worked apart)"))
	w := barWidth(20)
	for _, y := range years {
		var b strings.Builder
		total := 0.0
		for i, sname := range series {
			v := perYear[y][sname]
			total += v
			if v > 0 && maxTotal > 0 {
				cells := int(math.Round(v / maxTotal * float64(w)))
				b.WriteString(colors[i%len(colors)](strings.Repeat("█", max(cells, 1))))
			}
		}
		fmt.Fprintf(c.out, "%s %s %s\n", padRight(strconv.Itoa(y), 5), b.String(), dim(fnum(hoursOf(total))+" h"))
	}
	fmt.Fprintln(c.out)
	for i, sname := range series {
		fmt.Fprintf(c.out, "  %s %s", colors[i%len(colors)]("█"), sname)
	}
	fmt.Fprintln(c.out)
	return nil
}

var monthNames = []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}

// statsHoursProgress: cumulated hours month by month, one column per year, this year stops at this month
func statsHoursProgress(c *Ctx) error {
	s, err := needTimesheetStats(c)
	if err != nil {
		return err
	}
	years, clients, rows := timesheetRows(s)
	client := ""
	if v, ok := c.flag("client"); ok {
		for _, cl := range clients {
			if strings.Contains(strings.ToLower(cl), strings.ToLower(v)) {
				client = cl
			}
		}
		if client == "" {
			return fmt.Errorf("no client matching %q (%s)", v, strings.Join(clients, ", "))
		}
	}
	now := time.Now().In(brussels)
	cum := map[int][]float64{}
	for _, y := range years {
		total := 0.0
		for m := 1; m <= 12; m++ {
			for _, r := range rows {
				if r.year == y && r.month == m && (client == "" || r.client == client) {
					total += r.minutes
				}
			}
			if y > now.Year() || y == now.Year() && m > int(now.Month()) {
				cum[y] = append(cum[y], -1)
			} else {
				cum[y] = append(cum[y], hoursOf(total))
			}
		}
	}
	if c.jsonOut {
		return c.printJSON(cum)
	}
	title := "Cumulated hours"
	if client != "" {
		title += " · " + client
	}
	fmt.Fprintf(c.out, "%s  %s\n\n", bold(title), dim("month by month, one column per year · one client: --client"))
	head := padRight("", 5)
	for _, y := range years {
		head += padLeft(strconv.Itoa(y), 9)
	}
	fmt.Fprintln(c.out, dim(head))
	for m := 0; m < 12; m++ {
		line := padRight(monthNames[m], 5)
		for _, y := range years {
			v := cum[y][m]
			cell := ""
			if v >= 0 {
				cell = fnum(v)
			}
			if y == now.Year() {
				cell = bold(cell)
			}
			line += padLeft(cell, 9)
		}
		fmt.Fprintln(c.out, line)
	}
	// this year against the same months of the previous one
	if len(years) > 1 && years[len(years)-1] == now.Year() {
		m := int(now.Month()) - 1
		cur, prev := cum[now.Year()][m], cum[now.Year()-1]
		if prev != nil && prev[m] > 0 {
			fmt.Fprintf(c.out, "\n%s %s %s\n", dim("this year so far vs the same months of"), dim(strconv.Itoa(now.Year()-1)+":"), pctLabel(cur/prev[m]-1))
		}
	}
	return nil
}

// statsHoursClients: hours per client and year, most worked first, with the progress of this year
func statsHoursClients(c *Ctx) error {
	s, err := needTimesheetStats(c)
	if err != nil {
		return err
	}
	if c.jsonOut {
		return c.printJSON(s)
	}
	years, clients, rows := timesheetRows(s)
	now := time.Now().In(brussels)
	sum := func(f func(tsRow) bool) float64 {
		t := 0.0
		for _, r := range rows {
			if f(r) {
				t += r.minutes
			}
		}
		return t
	}
	row := func(client string) []string {
		of := func(r tsRow) bool { return client == "" || r.client == client }
		label := client
		if client == "" {
			label = bold("Total")
		}
		out := []string{label}
		for _, y := range years {
			h := hoursOf(sum(func(r tsRow) bool { return of(r) && r.year == y }))
			out = append(out, mark(h > 0, fnum(h)))
		}
		cur := sum(func(r tsRow) bool { return of(r) && r.year == now.Year() && r.month <= int(now.Month()) })
		prev := sum(func(r tsRow) bool { return of(r) && r.year == now.Year()-1 && r.month <= int(now.Month()) })
		progress := ""
		if prev > 0 && len(years) > 0 && years[len(years)-1] == now.Year() {
			progress = pctLabel(cur/prev - 1)
		}
		return append(out, progress)
	}
	head := []string{"client"}
	for _, y := range years {
		head = append(head, strconv.Itoa(y))
	}
	head = append(head, "progress")
	var tableRows [][]string
	for _, cl := range clients {
		tableRows = append(tableRows, row(cl))
	}
	tableRows = append(tableRows, row(""))
	fmt.Fprintf(c.out, "%s  %s\n\n", bold("Hours per client"), dim(fmt.Sprintf("progress: %d so far vs the same months of %d", now.Year(), now.Year()-1)))
	table(c.out, head, tableRows)
	return nil
}

// ---------- dossiers and expenses ----------

func statsDossiers(c *Ctx) error {
	var l []M
	if err := c.api.Post("/api/dossier/find-all-summaries", Q("closed", true), nil, &l); err != nil {
		return err
	}
	sort.SliceStable(l, func(i, j int) bool {
		a, _ := toTime(get(l[i], "closedDate"))
		b, _ := toTime(get(l[j], "closedDate"))
		return a.Before(b)
	})
	if c.jsonOut {
		return c.printJSON(l)
	}
	maxV := 0.0
	nameW := 8
	for _, d := range l {
		maxV = max(maxV, num(d, "totalEarnings"), num(d, "totalExpenses"))
		nameW = max(nameW, min(24, runeWidth(str(d, "name"))))
	}
	fmt.Fprintf(c.out, "%s  %s %s  %s %s\n\n", bold("Dossiers"), green("█"), dim("earnings"), red("█"), dim("expenses"))
	if len(l) == 0 {
		fmt.Fprintln(c.out, dim("no closed dossier"))
		return nil
	}
	w := barWidth(nameW + 18)
	for _, d := range l {
		e, x := num(d, "totalEarnings"), num(d, "totalExpenses")
		fmt.Fprintf(c.out, "%s %s %s\n", padRight(truncate(str(d, "name"), 24), nameW), padLeft(money(e), 14), green(bar(e, maxV, w)))
		fmt.Fprintf(c.out, "%s %s %s\n", padRight("", nameW), padLeft(money(x), 14), red(bar(x, maxV, w)))
	}
	return nil
}

func statsExpenses(c *Ctx) error {
	var l []M
	if err := c.api.Post("/api/fee/summaries", nil, nil, &l); err != nil {
		return err
	}
	var rows []M
	for _, s := range l {
		if !strings.EqualFold(str(s, "tag"), "other") && num(s, "totalHVAT") != 0 {
			rows = append(rows, s)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return num(rows[i], "totalHVAT") > num(rows[j], "totalHVAT") })
	if c.jsonOut {
		return c.printJSON(rows)
	}
	maxV, total := 0.0, 0.0
	nameW := 8
	for _, s := range rows {
		maxV = max(maxV, num(s, "totalHVAT"))
		total += num(s, "totalHVAT")
		nameW = max(nameW, min(24, runeWidth(str(s, "tag"))))
	}
	fmt.Fprintf(c.out, "%s  %s\n\n", bold("Expenses per tag"), dim("excl. VAT (vat aside), the tag OTHER left out"))
	w := barWidth(nameW + 34)
	for _, s := range rows {
		fmt.Fprintf(c.out, "%s %s %s %s\n", padRight(truncate(str(s, "tag"), 24), nameW), padLeft(moneyOf(s, "totalHVAT"), 14), padRight(yellow(bar(num(s, "totalHVAT"), maxV, w)), w), dim("vat "+moneyOf(s, "totalVAT")))
	}
	fmt.Fprintf(c.out, "%s %s\n", padRight("", nameW), padLeft(bold(money(total)), 14))
	return nil
}
