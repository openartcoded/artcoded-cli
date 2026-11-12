package main

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

func init() {
	register(&Module{
		Name: "timesheets", Aliases: []string{"timesheet", "ts"}, Title: "Timesheets", Routes: []string{"timesheets"},
		Help: "log hours, close timesheets, generate invoices",
		Actions: []*Action{
			{Name: "list", Aliases: []string{"ls"}, Help: "timesheets by year and client", Usage: "[--year 2026 --client name --open]", Run: tsList},
			{Name: "show", Help: "periods of a timesheet", Usage: "<id> [--all-days]", Run: tsShow},
			{Name: "log", Aliases: []string{"set", "fill"}, Help: "log the hours of a day (today by default)", Usage: "[<hours>] [--date YYYY-MM-DD --type WORKING_DAY --description .. --project .. --id <timesheet>]", Run: tsLog},
			{Name: "rate", Help: "change the rate of a day", Usage: "<id> --date YYYY-MM-DD --rate <rate id>", Run: tsRate},
			{Name: "settings", Help: "min / max hours per day of a timesheet", Usage: "<id> --min 0 --max 8", Run: tsSettings},
			{Name: "estimate", Help: "total to invoice (this month, or of a timesheet with --id)", Run: tsEstimate},
			{Name: "stats", Help: "hours worked per year, month and client", Usage: "[--year 2026]", Run: tsStats},
			{Name: "close", Help: "close a timesheet (generates its pdf)", Usage: "<id>", Run: func(c *Ctx) error { return tsSimple(c, "close", "/api/timesheet/close", "Close this timesheet?") }},
			{Name: "reopen", Help: "reopen a closed timesheet", Usage: "<id>", Run: func(c *Ctx) error { return tsSimple(c, "reopen", "/api/timesheet/reopen", "Reopen this timesheet?") }},
			{Name: "invoice", Aliases: []string{"generate-invoice"}, Help: "generate the invoice of a closed timesheet", Usage: "<id>", Run: func(c *Ctx) error {
				return tsSimple(c, "invoice generated", "/api/timesheet/generate-invoice", "Generate the invoice of this timesheet?")
			}},
			{Name: "download", Aliases: []string{"pdf"}, Help: "pdf of a closed timesheet", Usage: "<id> [--out path --open]", Run: tsDownload},
			{Name: "delete", Aliases: []string{"rm"}, Help: "delete a timesheet", Usage: "<id>", Run: tsDelete},
		},
	})
}

var periodTypes = []string{"WORKING_DAY", "SICKNESS", "PUBLIC_HOLIDAYS", "AUTHORIZED_HOLIDAYS", "WEEKEND"}

func loadTimesheets(c *Ctx) ([]M, error) {
	var grouped map[string]map[string][]M
	if err := c.api.Get("/api/timesheet", nil, &grouped); err != nil {
		return nil, err
	}
	var out []M
	for _, byClient := range grouped {
		for _, l := range byClient {
			out = append(out, l...)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := num(out[i], "year")*100+num(out[i], "month"), num(out[j], "year")*100+num(out[j], "month")
		if a != b {
			return a > b
		}
		return str(out[i], "clientName") < str(out[j], "clientName")
	})
	return out, nil
}

func tsDesc(t M) string {
	s := fmt.Sprintf("%s  %s  %sh", str(t, "name"), str(t, "clientName"), str(t, "numberOfHoursWorked"))
	if boolean(t, "closed") {
		s += dim(" (closed)")
	}
	return s
}

func tsList(c *Ctx) error {
	l, err := loadTimesheets(c)
	if err != nil {
		return err
	}
	year, client := c.quiet("year", ""), strings.ToLower(c.quiet("client", ""))
	var shown []M
	for _, t := range l {
		if year != "" && str(t, "year") != year || client != "" && !strings.Contains(strings.ToLower(str(t, "clientName")), client) {
			continue
		}
		if c.boolFlag("open") && boolean(t, "closed") {
			continue
		}
		shown = append(shown, t)
	}
	return c.result(shown, func() {
		rows := [][]string{}
		for _, t := range shown {
			state := yellow("open")
			if boolean(t, "closed") {
				state = dim("closed")
			}
			rows = append(rows, []string{str(t, "name"), str(t, "clientName"), str(t, "numberOfHoursWorked"), str(t, "numberOfWorkingDays"), state, mark(str(t, "invoiceId") != "", "✓"), dim(str(t, "id"))})
		}
		table(c.out, []string{"timesheet", "client", "hours", "working days", "", "invoiced", "id"}, rows)
	})
}

func pickTimesheet(c *Ctx, onlyOpen bool) (M, error) {
	m, err := c.pick("timesheet", func() ([]M, error) {
		l, err := loadTimesheets(c)
		if err != nil || !onlyOpen {
			return l, err
		}
		var open []M
		for _, t := range l {
			if !boolean(t, "closed") {
				open = append(open, t)
			}
		}
		return open, nil
	}, tsDesc)
	if err != nil {
		return nil, err
	}
	var t M
	if err := c.api.Get("/api/timesheet/by-id", Q("id", str(m, "id")), &t); err != nil {
		return nil, err
	}
	return t, nil
}

func periodMinutes(p M) float64 {
	if d := get(p, "duration"); d != nil {
		return num(p, "duration")
	}
	return 0
}

func tsShow(c *Ctx) error {
	t, err := pickTimesheet(c, false)
	if err != nil {
		return err
	}
	return c.result(t, func() {
		details(c.out, "timesheet", bold(str(t, "name")), "client", str(t, "clientName"), "closed", yesNo(boolean(t, "closed")),
			"hours", str(t, "numberOfHoursWorked"), "working days", str(t, "numberOfWorkingDays"),
			"min / max hours", fmt.Sprintf("%s / %s", str(t, "settings.minHoursPerDay"), str(t, "settings.maxHoursPerDay")), "id", str(t, "id"))
		rows := [][]string{}
		for _, p := range maps(t["periods"]) {
			if !c.boolFlag("all-days") && str(p, "periodType") == "WEEKEND" && periodMinutes(p) == 0 {
				continue
			}
			typ := str(p, "periodType")
			if typ == "WORKING_DAY" {
				typ = ""
			}
			hours := ""
			if m := periodMinutes(p); m > 0 {
				hours = strconv.FormatFloat(m/60, 'f', -1, 64)
			}
			rows = append(rows, []string{fdate(p, "date"), strings.ToLower(str(p, "dayOfWeek"))[:min(3, len(str(p, "dayOfWeek")))], hours, typ, str(p, "shortDescription"), str(p, "projectName")})
		}
		fmt.Fprintln(c.out)
		table(c.out, []string{"date", "day", "hours", "type", "description", "project"}, rows)
	})
}

// times of a day worked n hours, like the backoffice: morning from 8 (max >= 8) or 9, lunch 12-13
func timesFromHours(hours float64, maxHoursPerDay float64) ([4]int, error) {
	start := 9 * 60
	if maxHoursPerDay >= 8 {
		start = 8 * 60
	}
	const lunch, afternoon = 12 * 60, 13 * 60
	minutes := int(math.Round(hours * 60))
	maxMinutes := lunch - start + (23*60 + 59 - afternoon)
	if minutes <= 0 || minutes > maxMinutes {
		return [4]int{}, fmt.Errorf("%v hours do not fit in a day", hours)
	}
	morning := min(minutes, lunch-start)
	return [4]int{start, start + morning, afternoon, afternoon + minutes - morning}, nil
}

func atMinutes(day time.Time, m int) string {
	return iso(time.Date(day.Year(), day.Month(), day.Day(), m/60, m%60, 0, 0, brussels))
}

func tsLog(c *Ctx) error {
	day, err := c.date("date", "Day", "today", true)
	if err != nil {
		return err
	}
	var t M
	if _, ok := c.flag("id"); ok {
		if t, err = pickTimesheet(c, true); err != nil {
			return err
		}
	} else {
		// the open timesheets of that month; asked when several clients
		all, err := loadTimesheets(c)
		if err != nil {
			return err
		}
		var candidates []M
		for _, x := range all {
			if !boolean(x, "closed") && int(num(x, "year")) == day.Year() && int(num(x, "month")) == int(day.Month()) {
				candidates = append(candidates, x)
			}
		}
		if client := strings.ToLower(c.quiet("client", "")); client != "" {
			var f []M
			for _, x := range candidates {
				if strings.Contains(strings.ToLower(str(x, "clientName")), client) {
					f = append(f, x)
				}
			}
			candidates = f
		}
		switch {
		case len(candidates) == 0:
			return fmt.Errorf("no open timesheet for %s (timesheets are created each month for the ongoing clients)", day.Format("2006-01"))
		case len(candidates) == 1:
			t = candidates[0]
		case !c.interactive:
			return errors.New("several open timesheets this month: add --client or --id")
		default:
			opts := make([]string, len(candidates))
			for i, x := range candidates {
				opts[i] = tsDesc(x)
			}
			i, err := choose("Timesheet", opts)
			if err != nil {
				return err
			}
			t = candidates[i]
		}
		if err := c.api.Get("/api/timesheet/by-id", Q("id", str(t, "id")), &t); err != nil {
			return err
		}
	}
	var period M
	for _, p := range maps(t["periods"]) {
		if pt, ok := toTime(get(p, "date")); ok && pt.Format("2006-01-02") == day.Format("2006-01-02") {
			period = p
		}
	}
	if period == nil {
		return fmt.Errorf("%s is not a day of timesheet %s", day.Format("2006-01-02"), str(t, "name"))
	}
	currentType := str(period, "periodType")
	if currentType == "" || currentType == "WEEKEND" && periodMinutes(period) == 0 {
		currentType = "WORKING_DAY"
	}
	typ, err := c.enum("type", "Type of day", periodTypes, currentType)
	if err != nil {
		return err
	}
	maxH := num(t, "settings.maxHoursPerDay")
	period["periodType"] = typ
	if typ == "WORKING_DAY" || typ == "WEEKEND" {
		var defH *float64
		if m := periodMinutes(period); m > 0 {
			f := m / 60
			defH = &f
		} else if maxH > 0 {
			f := maxH
			defH = &f
		}
		if h, ok := c.pos(); ok {
			c.args.flags["hours"] = []string{h}
		}
		hv, err := c.num("hours", "Hours worked (0 to clear)", defH, true)
		if err != nil {
			return err
		}
		if *hv == 0 {
			for _, k := range []string{"morningStartTime", "morningEndTime", "afternoonStartTime", "afternoonEndTime"} {
				period[k] = nil
			}
		} else {
			tm, err := timesFromHours(*hv, maxH)
			if err != nil {
				return err
			}
			period["morningStartTime"] = atMinutes(*day, tm[0])
			period["morningEndTime"] = atMinutes(*day, tm[1])
			period["afternoonStartTime"] = atMinutes(*day, tm[2])
			period["afternoonEndTime"] = atMinutes(*day, tm[3])
		}
		if d, err := c.opt("description", "Description", str(period, "shortDescription")); err != nil {
			return err
		} else {
			period["shortDescription"] = d
		}
		defProject := str(period, "projectName")
		if defProject == "" {
			defProject = str(t, "settings.defaultProjectName")
		}
		if v, ok := c.flag("project"); ok {
			period["projectName"] = v
		} else if defProject != "" {
			period["projectName"] = defProject
		}
	} else {
		for _, k := range []string{"morningStartTime", "morningEndTime", "afternoonStartTime", "afternoonEndTime"} {
			period[k] = nil
		}
	}
	var saved M
	if err := c.api.Post("/api/timesheet/save-period", Q("id", str(t, "id")), period, &saved); err != nil {
		return err
	}
	return c.result(saved, func() {
		info("%s %s: %s %s", str(t, "name"), day.Format("Mon 2006-01-02"), strings.ToLower(typ), str(saved, "durationInHours"))
	})
}

func tsRate(c *Ctx) error {
	t, err := pickTimesheet(c, false)
	if err != nil {
		return err
	}
	day, err := c.date("date", "Day", "today", true)
	if err != nil {
		return err
	}
	var period M
	for _, p := range maps(t["periods"]) {
		if pt, ok := toTime(get(p, "date")); ok && pt.Format("2006-01-02") == day.Format("2006-01-02") {
			period = p
		}
	}
	if period == nil {
		return fmt.Errorf("%s is not a day of this timesheet", day.Format("2006-01-02"))
	}
	rateID := c.quiet("rate", "")
	if rateID == "" {
		var clients []M
		if err := c.api.Get("/api/billable-client/find-all", nil, &clients); err != nil {
			return err
		}
		var rates []M
		for _, cl := range clients {
			if str(cl, "id") == str(t, "clientId") {
				rates = maps(cl["rates"])
			}
		}
		if len(rates) == 0 {
			return errors.New("the client of this timesheet has no rate")
		}
		if !c.interactive {
			return missing("rate")
		}
		opts := make([]string, len(rates))
		for i, r := range rates {
			opts[i] = fmt.Sprintf("%s %s€/%s", str(r, "label"), str(r, "rate"), strings.ToLower(str(r, "rateType")))
			if i == 0 {
				opts[i] += dim(" (default)")
			}
		}
		i, err := choose("Rate", opts)
		if err != nil {
			return err
		}
		rateID = str(rates[i], "id")
	}
	if err := c.api.Post("/api/timesheet/period-rate", Q("id", str(t, "id"), "periodId", str(period, "id"), "rateId", rateID), nil, nil); err != nil {
		return err
	}
	info("rate updated")
	return nil
}

func tsSettings(c *Ctx) error {
	t, err := pickTimesheet(c, false)
	if err != nil {
		return err
	}
	var cur struct{ min, max *float64 }
	if v := get(t, "settings.minHoursPerDay"); v != nil {
		f := num(t, "settings.minHoursPerDay")
		cur.min = &f
	}
	if v := get(t, "settings.maxHoursPerDay"); v != nil {
		f := num(t, "settings.maxHoursPerDay")
		cur.max = &f
	}
	mn, err := c.num("min", "Min hours per day", cur.min, true)
	if err != nil {
		return err
	}
	mx, err := c.num("max", "Max hours per day", cur.max, true)
	if err != nil {
		return err
	}
	var saved M
	if err := c.api.Post("/api/timesheet/settings", nil, M{"timesheetId": str(t, "id"), "clientId": str(t, "clientId"), "minHoursPerDay": mn, "maxHoursPerDay": mx}, &saved); err != nil {
		return err
	}
	info("settings saved")
	return nil
}

func tsEstimate(c *Ctx) error {
	var v any
	if id, ok := c.flagOrPos("id"); ok {
		if err := c.api.Get("/api/timesheet/estimate-total", Q("id", id), &v); err != nil {
			return err
		}
	} else if err := c.api.Get("/api/timesheet/estimate-total-to-be-invoiced-this-month", nil, &v); err != nil {
		return err
	}
	return c.result(v, func() {
		f, _ := v.(float64)
		fmt.Fprintln(c.out, money(f))
	})
}

func tsStats(c *Ctx) error {
	var s M
	if err := c.api.Get("/api/timesheet/stats", nil, &s); err != nil {
		return err
	}
	return c.result(s, func() {
		year := c.quiet("year", "")
		rows := [][]string{}
		for _, r := range maps(s["rows"]) {
			if year != "" && str(r, "year") != year {
				continue
			}
			rows = append(rows, []string{str(r, "year"), fmt.Sprintf("%02d", int(num(r, "month"))), str(r, "client"),
				strconv.FormatFloat(num(r, "minutes")/60, 'f', 1, 64), str(r, "days")})
		}
		table(c.out, []string{"year", "month", "client", "hours", "days"}, rows)
		if wd, ok := s["workedDays"].(map[string]any); ok && len(wd) > 0 {
			fmt.Fprintln(c.out, "\n"+bold("Worked days per year"))
			for _, k := range sortedKeys(wd) {
				fmt.Fprintf(c.out, "  %s  %v\n", k, wd[k])
			}
		}
	})
}

func tsSimple(c *Ctx, done, path, question string) error {
	t, err := pickTimesheet(c, false)
	if err != nil {
		return err
	}
	if err := c.confirmAction(question + " (" + str(t, "name") + " " + str(t, "clientName") + ")"); err != nil {
		return err
	}
	if err := c.api.Post(path, Q("id", str(t, "id")), nil, nil); err != nil {
		return err
	}
	info("%s: %s", str(t, "name"), done)
	return nil
}

func tsDownload(c *Ctx) error {
	t, err := pickTimesheet(c, false)
	if err != nil {
		return err
	}
	if str(t, "uploadId") == "" {
		return errors.New("no pdf yet: close the timesheet first")
	}
	d, err := c.api.FileDownload(str(t, "uploadId"))
	if err != nil {
		return err
	}
	return c.save(d)
}

func tsDelete(c *Ctx) error {
	t, err := pickTimesheet(c, false)
	if err != nil {
		return err
	}
	if err := c.confirmAction("Delete timesheet " + str(t, "name") + " " + str(t, "clientName") + "?"); err != nil {
		return err
	}
	if err := c.api.Delete("/api/timesheet", Q("id", str(t, "id")), nil); err != nil {
		return err
	}
	info("deleted")
	return nil
}
