package main

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// dashboard: the figures checked every day, like the home page of the backoffice, in tables and charts.
func dashboard(c *Ctx) error {
	now := time.Now().In(brussels)
	var (
		me, active, personal  M
		notifs, todos, tasks  []M
		summaries, timesheets []M
		estimate              any
		feePage               M
		tsStats               M
		errMe                 error
		wg                    sync.WaitGroup
	)
	// independent requests, in parallel; the ones the role cannot do are left out
	run := func(f func()) { wg.Add(1); go func() { defer wg.Done(); f() }() }
	run(func() { errMe = c.api.Get("/api/personal-info/@me", nil, &me) })
	run(func() { _ = c.api.Get("/api/personal-info", nil, &personal) })
	run(func() { _ = c.api.Get("/api/notification", nil, &notifs) })
	run(func() { _ = c.api.Get("/api/todo", nil, &todos) })
	run(func() { _ = c.api.Get("/api/reminder-task/find-next-ten-tasks", nil, &tasks) })
	run(func() { _ = c.api.Get("/api/timesheet/estimate-total-to-be-invoiced-this-month", nil, &estimate) })
	run(func() { _ = c.api.Post("/api/dossier/active-dossier", nil, nil, &active) })
	run(func() { _ = c.api.Post("/api/invoice/find-all-summaries", nil, nil, &summaries) })
	run(func() {
		_ = c.api.Post("/api/fee/search", Q("page", 0, "size", 1, "sort", "date,DESC"), M{"archived": false, "bookmarked": false}, &feePage)
	})
	run(func() { _ = c.api.Get("/api/timesheet/stats", nil, &tsStats) })
	run(func() { timesheets, _ = loadTimesheets(c) })
	wg.Wait()
	if errMe != nil {
		return errMe
	}
	refreshMenuCache(c.api, c.cfg, strList(me, "authorities"))
	// the demo mode of the backoffice masks this screen too
	if boolean(personal, "demoMode") && !c.has("demo") {
		demoMode = true
	}
	if c.jsonOut {
		return c.printJSON(M{"user": me, "notifications": notifs, "todos": todos, "nextTasks": tasks, "estimateThisMonth": estimate,
			"activeDossier": active, "invoiceSummaries": summaries, "unprocessedExpenses": feePage, "timesheetStats": tsStats})
	}
	isAdmin := false
	for _, r := range strList(me, "authorities") {
		isAdmin = isAdmin || r == "ADMIN"
	}

	greeting := "Good evening"
	switch h := now.Hour(); {
	case h < 12:
		greeting = "Good morning"
	case h < 18:
		greeting = "Good afternoon"
	}
	title := bold(greeting+", "+str(me, "username")) + "  " + dim(now.Format("Monday 2 January 2006"))
	if demoMode {
		title += "  " + yellow("demo")
	}
	fmt.Fprintln(c.out, title+"\n")

	// ---- key figures
	year := now.Year()
	var revenue, revenuePrev, days float64
	invoices := 0
	sameDateLastYear := now.AddDate(-1, 0, 0)
	for _, s := range summaries {
		t, ok := toTime(get(s, "dateOfInvoice"))
		if !ok {
			continue
		}
		switch {
		case t.Year() == year:
			revenue += num(s, "subTotal")
			days += amountInDays(s)
			invoices++
		case t.Year() == year-1 && !t.After(sameDateLastYear):
			revenuePrev += num(s, "subTotal")
		}
	}
	var kpis [][]string
	if f, ok := estimate.(float64); ok && isAdmin {
		kpis = append(kpis, []string{"To invoice this month", money(f), dim("from the open timesheets")})
	}
	if summaries != nil {
		detail := fmt.Sprintf("%d invoice(s)", invoices)
		if revenuePrev > 0 {
			detail += " · " + pctLabel(revenue/revenuePrev-1) + dim(fmt.Sprintf(" vs %d at the same date", year-1))
		}
		kpis = append(kpis, []string{fmt.Sprintf("Revenue %d (excl. VAT)", year), money(revenue), detail})
		perDay := ""
		if days > 0 {
			perDay = dim(money(revenue/days) + " per day invoiced")
		}
		kpis = append(kpis, []string{fmt.Sprintf("Days invoiced %d", year), fnum(math.Round(days*10) / 10), perDay})
	}
	if tsStats != nil {
		_, _, rows := timesheetRows(tsStats)
		var cur, prev float64
		for _, r := range rows {
			if r.month == int(now.Month()) && r.year == year {
				cur += r.minutes
			}
			if r.month == int(now.Month()) && r.year == year-1 {
				prev += r.minutes
			}
		}
		detail := ""
		if prev > 0 {
			detail = dim(fmt.Sprintf("%s h in %s %d", fnum(hoursOf(prev)), now.Format("January"), year-1))
		}
		kpis = append(kpis, []string{"Hours this month", fnum(hoursOf(cur)) + " h", detail})
	}
	if feePage != nil {
		n := 0.0
		if pg, ok := feePage["page"].(map[string]any); ok {
			n = num(pg, "totalElements")
		} else {
			n = num(feePage, "totalElements")
		}
		detail := dim("all processed")
		if l := content(feePage); len(l) > 0 {
			detail = dim("latest: " + truncate(str(l[0], "subject"), 40))
		}
		kpis = append(kpis, []string{"Unprocessed expenses", strconv.Itoa(int(n)), detail})
	}
	if active != nil && str(active, "name") != "" {
		kpis = append(kpis, []string{"Active dossier", str(active, "name"), dim("VAT estimate " + moneyOf(active, "tvaToBePaid"))})
	} else {
		kpis = append(kpis, []string{"Active dossier", "none", dim("open one: dossiers new")})
	}
	unseen := 0
	for _, n := range notifs {
		if !boolean(n, "seen") {
			unseen++
		}
	}
	kpis = append(kpis, []string{"Unseen notifications", strconv.Itoa(unseen), dim(fmt.Sprintf("of %d", len(notifs)))})
	table(c.out, []string{"", "", ""}, kpis)

	// ---- revenue per month, last 12 months
	if len(summaries) > 0 {
		perMonth := map[string]float64{}
		for _, s := range summaries {
			if t, ok := toTime(get(s, "dateOfInvoice")); ok {
				perMonth[t.Format("2006-01")] += num(s, "subTotal")
			}
		}
		var labels []string
		var values []float64
		for i := 11; i >= 0; i-- {
			m := time.Date(year, now.Month(), 1, 0, 0, 0, 0, brussels).AddDate(0, -i, 0)
			labels = append(labels, m.Format("Jan"))
			values = append(values, perMonth[m.Format("2006-01")])
		}
		fmt.Fprintf(c.out, "\n%s  %s\n", bold("Revenue per month"), dim("last 12 months, by date of invoice"))
		fmt.Fprint(c.out, columnChart(labels, values, 7, "€", cyan))

		// cumulated revenue: this year against the previous one
		cum := func(y int, upTo int) []float64 {
			out := make([]float64, 12)
			total := 0.0
			for m := 1; m <= 12; m++ {
				total += perMonth[fmt.Sprintf("%d-%02d", y, m)]
				out[m-1] = total
				if m > upTo {
					out[m-1] = math.NaN()
				}
			}
			return out
		}
		fmt.Fprintf(c.out, "\n%s  %s\n", bold("Cumulated revenue"), dim(fmt.Sprintf("%d against %d", year, year-1)))
		fmt.Fprint(c.out, lineChart([]Series{
			{Name: strconv.Itoa(year - 1), Values: cum(year-1, 12), Color: dim},
			{Name: strconv.Itoa(year), Values: cum(year, int(now.Month())), Color: cyan},
		}, monthNames, 48, 8, "€"))
	}

	// ---- open timesheets (admin)
	if isAdmin && len(timesheets) > 0 {
		var rows [][]string
		for _, t := range timesheets {
			if boolean(t, "closed") {
				continue
			}
			rows = append(rows, []string{str(t, "name"), sens(str(t, "clientName")), str(t, "numberOfHoursWorked") + " h", str(t, "numberOfWorkingDays"), dim(str(t, "id"))})
		}
		if len(rows) > 0 {
			fmt.Fprintln(c.out, "\n"+bold("Open timesheets"))
			table(c.out, []string{"timesheet", "client", "hours", "days", "id"}, rows)
		}
	}

	// ---- what to do
	if len(tasks) > 0 {
		sort.SliceStable(tasks, func(i, j int) bool { return str(tasks[i], "nextDate") < str(tasks[j], "nextDate") })
		var rows [][]string
		for i, t := range tasks {
			if i == 5 {
				break
			}
			rows = append(rows, []string{fdatetime(t, "nextDate"), str(t, "title"), dim(strings.ToLower(str(t, "actionKey")))})
		}
		fmt.Fprintln(c.out, "\n"+bold("Next tasks"))
		table(c.out, []string{"when", "task", "action"}, rows)
	}
	var open [][]string
	for _, t := range todos {
		if !boolean(t, "done") {
			open = append(open, []string{"☐", str(t, "title")})
		}
	}
	if len(open) > 0 {
		fmt.Fprintln(c.out, "\n"+bold("Todos"))
		table(c.out, []string{"", "todo"}, open)
	}
	if unseen > 0 {
		var rows [][]string
		for _, n := range notifs {
			if !boolean(n, "seen") && len(rows) < 5 {
				rows = append(rows, []string{fdatetime(n, "receivedDate"), str(n, "title"), dim(str(n, "type"))})
			}
		}
		fmt.Fprintln(c.out, "\n"+bold("Unseen notifications"))
		table(c.out, []string{"received", "notification", "type"}, rows)
	}
	return nil
}
