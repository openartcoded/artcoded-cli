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
			{Name: "revenue", Aliases: []string{"revenue-progress"}, Help: "cumulated revenue month by month, one line per year", Usage: "[--years 4]", Run: statsRevenue},
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
	if bot != nil {
		return bot.width
	}
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
	fmt.Fprintf(c.out, "%s  %s\n\n", bold("Per year"), dim(fmt.Sprintf("all time: %s · %s days · %d invoices", money(st.total), fnum(math.Round(st.days*10)/10), st.totalInvoices)))
	head := []string{"year", "revenue", "", "days", "per day"}
	if worked != nil {
		head = append(head, "relative")
	}
	head = append(head, "invoices", "vs previous")
	current := time.Now().In(brussels).Year()
	var rows [][]string
	for i, y := range years {
		ys := st.perYear[y]
		perDay := ""
		if ys.days > 0 {
			perDay = money(ys.revenue / ys.days)
		}
		label := strconv.Itoa(y)
		if y == current {
			label = bold(label)
		}
		row := []string{label, money(ys.revenue), cyan(bar(ys.revenue, maxRev, barWidth(100))), fnum(math.Round(ys.days*10) / 10), perDay}
		if worked != nil {
			rel := ""
			if d := num(worked, strconv.Itoa(y)); d > 0 {
				rel = money(ys.revenue / d)
			}
			row = append(row, rel)
		}
		evolution := ""
		if i+1 < len(years) && y != current {
			if prev := st.perYear[years[i+1]]; prev != nil && prev.revenue > 0 {
				evolution = pctLabel(ys.revenue/prev.revenue - 1)
			}
		}
		rows = append(rows, append(row, strconv.Itoa(ys.invoices), evolution))
	}
	table(c.out, head, rows)
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
	st, _, err := loadInvoiceStats(c)
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
	title := "Per month"
	if year != "" {
		title += " " + year
	}
	fmt.Fprintf(c.out, "%s  %s\n\n", bold(title), dim("revenue excl. VAT and days invoiced, by period of the invoices · other year: --year, every month: --all"))
	if len(rows) == 0 {
		fmt.Fprintln(c.out, dim("no invoice"))
		return nil
	}
	var labels []string
	var values []float64
	var total, days float64
	for _, p := range rows {
		label := p.period
		if year != "" {
			if mm, err := strconv.Atoi(strings.SplitN(p.period, "/", 2)[0]); err == nil && mm >= 1 && mm <= 12 {
				label = monthNames[mm-1]
			}
		}
		labels = append(labels, label)
		values = append(values, p.earnings)
		total += p.earnings
		days += p.days
	}
	if len(rows) <= 24 {
		fmt.Fprint(c.out, columnChart(labels, values, 8, "€", cyan))
		fmt.Fprintln(c.out)
	}
	var tr [][]string
	for _, p := range rows {
		perDay := ""
		if p.days > 0 {
			perDay = money(p.earnings / p.days)
		}
		tr = append(tr, []string{p.period, money(p.earnings), fnum(math.Round(p.days*10) / 10), perDay})
	}
	tr = append(tr, []string{bold("total"), bold(money(total)), bold(fnum(math.Round(days*10) / 10)), ""})
	table(c.out, []string{"period", "revenue", "days", "per day"}, tr)
	return nil
}

func statsClients(c *Ctx) error {
	st, raw, err := loadInvoiceStats(c)
	if err != nil {
		return err
	}
	per := st.perClient
	days := map[string]float64{}
	year := c.quiet("year", "")
	if year != "" {
		per = map[string]float64{}
	}
	for _, s := range raw {
		t, ok := toTime(get(s, "dateOfInvoice"))
		if year != "" && (!ok || strconv.Itoa(t.Year()) != year) {
			continue
		}
		if year != "" {
			per[defStr(str(s, "client"), "N/A")] += num(s, "subTotal")
		}
		days[defStr(str(s, "client"), "N/A")] += amountInDays(s)
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
	var tr [][]string
	for _, r := range rows {
		share, perDay := "", ""
		if total > 0 {
			share = fmt.Sprintf("%.0f%%", r.total/total*100)
		}
		if d := days[r.client]; d > 0 {
			perDay = money(r.total / d)
		}
		tr = append(tr, []string{sens(r.client), money(r.total), share, cyan(bar(r.total, maxT, barWidth(80))), fnum(math.Round(days[r.client]*10) / 10), perDay})
	}
	tr = append(tr, []string{bold("total"), bold(money(total)), "", "", "", ""})
	table(c.out, []string{"client", "revenue", "share", "", "days", "per day"}, tr)
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
		fmt.Fprintf(c.out, "  %s %s", colors[i%len(colors)]("█"), sens(sname))
	}
	fmt.Fprintln(c.out)
	fmt.Fprintln(c.out)
	head := append([]string{"year"}, series...)
	for i := 1; i < len(head); i++ {
		head[i] = sens(head[i])
	}
	head = append(head, "total")
	var tr [][]string
	for _, y := range years {
		row := []string{strconv.Itoa(y)}
		total := 0.0
		for _, sname := range series {
			v := perYear[y][sname]
			total += v
			row = append(row, mark(v > 0, fnum(hoursOf(v))))
		}
		tr = append(tr, append(row, bold(fnum(hoursOf(total)))))
	}
	table(c.out, head, tr)
	return nil
}

var monthNames = []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}

// statsHoursProgress: cumulated hours month by month, one line per year, this year stops at this month
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
				cum[y] = append(cum[y], math.NaN())
			} else {
				cum[y] = append(cum[y], hoursOf(total))
			}
		}
	}
	if c.jsonOut {
		out := map[string][]any{}
		for y, v := range cum {
			for _, x := range v {
				if math.IsNaN(x) {
					out[strconv.Itoa(y)] = append(out[strconv.Itoa(y)], nil)
				} else {
					out[strconv.Itoa(y)] = append(out[strconv.Itoa(y)], x)
				}
			}
		}
		return c.printJSON(out)
	}
	title := "Cumulated hours"
	if client != "" {
		title += " · " + sens(client)
	}
	fmt.Fprintf(c.out, "%s  %s\n\n", bold(title), dim("month by month, one line per year · one client: --client"))
	// the last years on the chart, every year in the table
	var series []Series
	shown := years
	if len(shown) > 5 {
		shown = shown[len(shown)-5:]
	}
	for i, y := range shown {
		color := seriesColors[i%len(seriesColors)]
		if y == now.Year() {
			color = cyan
		} else if y == now.Year()-1 {
			color = yellow
		} else {
			color = dim
		}
		series = append(series, Series{Name: strconv.Itoa(y), Values: cum[y], Color: color})
	}
	fmt.Fprint(c.out, lineChart(series, monthNames, 48, 10, "h"))
	fmt.Fprintln(c.out)
	head := []string{""}
	for _, y := range years {
		head = append(head, strconv.Itoa(y))
	}
	var tr [][]string
	for m := 0; m < 12; m++ {
		row := []string{monthNames[m]}
		for _, y := range years {
			cell := ""
			if v := cum[y][m]; !math.IsNaN(v) {
				cell = fnum(v)
			}
			row = append(row, cell)
		}
		tr = append(tr, row)
	}
	table(c.out, head, tr)
	if len(years) > 1 && years[len(years)-1] == now.Year() {
		m := int(now.Month()) - 1
		cur, prev := cum[now.Year()][m], cum[now.Year()-1]
		if prev != nil && prev[m] > 0 && !math.IsNaN(cur) {
			fmt.Fprintf(c.out, "\n%s %s\n", dim(fmt.Sprintf("%d so far vs the same months of %d:", now.Year(), now.Year()-1)), pctLabel(cur/prev[m]-1))
		}
	}
	return nil
}

// statsRevenue: cumulated revenue month by month (by date of invoice), one line per year
func statsRevenue(c *Ctx) error {
	_, raw, err := loadInvoiceStats(c)
	if err != nil {
		return err
	}
	now := time.Now().In(brussels)
	perMonth := map[int][]float64{}
	for _, s := range raw {
		t, ok := toTime(get(s, "dateOfInvoice"))
		if !ok {
			continue
		}
		if perMonth[t.Year()] == nil {
			perMonth[t.Year()] = make([]float64, 12)
		}
		perMonth[t.Year()][int(t.Month())-1] += num(s, "subTotal")
	}
	years := sortedYears(perMonth, false)
	if y := c.quiet("years", ""); y != "" {
		if n, err := strconv.Atoi(y); err == nil && n > 0 && n < len(years) {
			years = years[len(years)-n:]
		}
	} else if len(years) > 4 {
		years = years[len(years)-4:]
	}
	cum := map[int][]float64{}
	for _, y := range years {
		total := 0.0
		for m := 0; m < 12; m++ {
			total += perMonth[y][m]
			if y == now.Year() && m+1 > int(now.Month()) {
				cum[y] = append(cum[y], math.NaN())
			} else {
				cum[y] = append(cum[y], total)
			}
		}
	}
	if c.jsonOut {
		return c.printJSON(perMonth)
	}
	fmt.Fprintf(c.out, "%s  %s\n\n", bold("Cumulated revenue"), dim("excl. VAT, by date of invoice, one line per year · how many years: --years N"))
	if len(years) == 0 {
		fmt.Fprintln(c.out, dim("no invoice yet"))
		return nil
	}
	var series []Series
	for _, y := range years {
		color := dim
		switch y {
		case now.Year():
			color = cyan
		case now.Year() - 1:
			color = yellow
		}
		series = append(series, Series{Name: strconv.Itoa(y), Values: cum[y], Color: color})
	}
	fmt.Fprint(c.out, lineChart(series, monthNames, 48, 10, "€"))
	fmt.Fprintln(c.out)
	head := []string{""}
	for _, y := range years {
		head = append(head, strconv.Itoa(y))
	}
	var tr [][]string
	for m := 0; m < 12; m++ {
		row := []string{monthNames[m]}
		for _, y := range years {
			cell := ""
			if v := cum[y][m]; !math.IsNaN(v) {
				cell = money(v)
			}
			row = append(row, cell)
		}
		tr = append(tr, row)
	}
	table(c.out, head, tr)
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
		label := sens(client)
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
	fmt.Fprintf(c.out, "%s  %s %s  %s %s\n\n", bold("Dossiers"), green("█"), dim("earnings"), red("█"), dim("expenses"))
	if len(l) == 0 {
		fmt.Fprintln(c.out, dim("no closed dossier"))
		return nil
	}
	maxV := 0.0
	for _, d := range l {
		maxV = max(maxV, num(d, "totalEarnings"), num(d, "totalExpenses"))
	}
	w := barWidth(90)
	var tr [][]string
	for _, d := range l {
		e, x := num(d, "totalEarnings"), num(d, "totalExpenses")
		tr = append(tr, []string{str(d, "name"), fdate(d, "closedDate"), money(e), money(x), money(e - x), green(bar(e, maxV, w))})
		tr = append(tr, []string{"", "", "", "", "", red(bar(x, maxV, w))})
	}
	table(c.out, []string{"dossier", "closed", "earnings", "expenses", "balance", ""}, tr)
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
	maxV, total, vat := 0.0, 0.0, 0.0
	for _, s := range rows {
		maxV = max(maxV, num(s, "totalHVAT"))
		total += num(s, "totalHVAT")
		vat += num(s, "totalVAT")
	}
	fmt.Fprintf(c.out, "%s  %s\n\n", bold("Expenses per tag"), dim("excl. VAT, the tag OTHER left out like on the home page"))
	var tr [][]string
	for _, s := range rows {
		share := ""
		if total > 0 {
			share = fmt.Sprintf("%.0f%%", num(s, "totalHVAT")/total*100)
		}
		tr = append(tr, []string{str(s, "tag"), moneyOf(s, "totalHVAT"), moneyOf(s, "totalVAT"), share, yellow(bar(num(s, "totalHVAT"), maxV, barWidth(70)))})
	}
	tr = append(tr, []string{bold("total"), bold(money(total)), bold(money(vat)), "", ""})
	table(c.out, []string{"tag", "excl. vat", "vat", "share", ""}, tr)
	return nil
}
