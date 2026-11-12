package main

import (
	"fmt"
	"sort"
	"strings"
)

func init() {
	register(&Module{
		Name: "tasks", Aliases: []string{"task", "reminders"}, Title: "Tasks", Routes: []string{"tasks"},
		Help: "reminders and scheduled actions",
		Actions: []*Action{
			{Name: "list", Aliases: []string{"ls"}, Help: "every task, next first", Run: taskList},
			{Name: "next", Help: "the next ten tasks", Run: taskNext},
			{Name: "show", Help: "a task", Usage: "<id>", Run: taskShow},
			{Name: "add", Aliases: []string{"new", "create"}, Help: "add a reminder or a scheduled action", Usage: "[--title --description --action <key> --cron <expr> --date <YYYY-MM-DD HH:mm> --param key=value]", Run: taskAdd},
			{Name: "edit", Help: "edit a task (json in the editor)", Usage: "<id>", Run: taskEdit},
			{Name: "enable", Help: "enable a task", Usage: "<id>", Run: func(c *Ctx) error { return taskSetDisabled(c, false) }},
			{Name: "disable", Help: "disable a task", Usage: "<id>", Run: func(c *Ctx) error { return taskSetDisabled(c, true) }},
			{Name: "delete", Aliases: []string{"rm"}, Help: "delete a task", Usage: "<id>", Run: taskDelete},
			{Name: "actions", Help: "actions a task can run, with their parameters", Run: taskActions},
			{Name: "results", Help: "results of the runs of an action", Usage: "<action key> [--page n --size n]", Run: taskResults},
			{Name: "validate-cron", Help: "check a cron expression", Usage: "<expression>", Run: taskValidateCron},
		},
	})
}

func loadTasks(c *Ctx) ([]M, error) {
	var l []M
	return l, c.api.Get("/api/reminder-task/find-all", nil, &l)
}

func taskDesc(t M) string {
	s := str(t, "title")
	if boolean(t, "disabled") {
		s += dim(" (disabled)")
	}
	return s + "  " + dim(fdatetime(t, "nextDate"))
}

func taskRows(l []M) [][]string {
	rows := [][]string{}
	for _, t := range l {
		when := str(t, "cronExpression")
		if when == "" {
			when = fdatetime(t, "specificDate")
		}
		state := green("on")
		if boolean(t, "disabled") {
			state = dim("off")
		}
		rows = append(rows, []string{state, str(t, "title"), str(t, "actionKey"), when, fdatetime(t, "nextDate"), fdatetime(t, "lastExecutionDate"), dim(str(t, "id"))})
	}
	return rows
}

func taskList(c *Ctx) error {
	l, err := loadTasks(c)
	if err != nil {
		return err
	}
	return c.result(l, func() {
		table(c.out, []string{"", "title", "action", "cron / date", "next", "last run", "id"}, taskRows(l))
	})
}

func taskNext(c *Ctx) error {
	var l []M
	if err := c.api.Get("/api/reminder-task/find-next-ten-tasks", nil, &l); err != nil {
		return err
	}
	return c.result(l, func() {
		table(c.out, []string{"", "title", "action", "cron / date", "next", "last run", "id"}, taskRows(l))
	})
}

func fetchTask(c *Ctx) (M, error) {
	id, err := c.pickID("task", func() ([]M, error) { return loadTasks(c) }, taskDesc)
	if err != nil {
		return nil, err
	}
	var t M
	if err := c.api.Get("/api/reminder-task/find-by-id", Q("id", id), &t); err != nil {
		return nil, err
	}
	if t == nil {
		return nil, fmt.Errorf("task %s not found", id)
	}
	return t, nil
}

func taskShow(c *Ctx) error {
	t, err := fetchTask(c)
	if err != nil {
		return err
	}
	return c.result(t, func() {
		details(c.out, "title", bold(str(t, "title")), "description", str(t, "description"), "id", str(t, "id"),
			"disabled", yesNo(boolean(t, "disabled")), "action", str(t, "actionKey"), "custom name", str(t, "customActionName"),
			"cron", str(t, "cronExpression"), "date", fdatetime(t, "specificDate"), "calendar date", fdatetime(t, "calendarDate"),
			"next", fdatetime(t, "nextDate"), "last run", fdatetime(t, "lastExecutionDate"),
			"mail", yesNo(boolean(t, "sendMail")), "sms", yesNo(boolean(t, "sendSms")),
			"in app", yesNo(boolean(t, "inAppNotification")), "keep results", yesNo(boolean(t, "persistResult")))
		for _, p := range maps(t["actionParameters"]) {
			fmt.Fprintf(c.out, "  %s = %s\n", dim(str(p, "key")), str(p, "value"))
		}
	})
}

func loadActions(c *Ctx) ([]M, error) {
	var l []M
	return l, c.api.Get("/api/reminder-task/allowed-actions", nil, &l)
}

func validCron(c *Ctx, expr string) bool {
	var r M
	if err := c.api.Get("/api/reminder-task/validate-cron-expression", Q("cronExpression", expr), &r); err != nil {
		return true // let the backend refuse on save
	}
	return boolean(r, "valid")
}

func taskAdd(c *Ctx) error {
	title, err := c.reqPos("title", "Title", "")
	if err != nil {
		return err
	}
	desc, err := c.opt("description", "Description", "")
	if err != nil {
		return err
	}
	task := M{"title": title, "description": desc, "disabled": false}

	// an action, or a plain reminder
	actionKey := c.quiet("action", "")
	var action M
	if actionKey == "" && c.interactive && !c.yes {
		kind, err := c.enum("kind", "What kind of task?", []string{"reminder", "action"}, "reminder")
		if err != nil {
			return err
		}
		if kind == "action" {
			actions, err := loadActions(c)
			if err != nil {
				return err
			}
			opts := make([]string, len(actions))
			for i, a := range actions {
				opts[i] = str(a, "title") + "  " + dim(str(a, "key"))
			}
			i, err := choose("Action", opts)
			if err != nil {
				return err
			}
			action = actions[i]
			actionKey = str(action, "key")
		}
	} else if actionKey != "" {
		actions, err := loadActions(c)
		if err != nil {
			return err
		}
		for _, a := range actions {
			if strings.EqualFold(str(a, "key"), actionKey) {
				action = a
				actionKey = str(a, "key")
			}
		}
		if action == nil {
			return fmt.Errorf("unknown action %q (tasks actions)", actionKey)
		}
	}
	if action != nil {
		task["actionKey"] = actionKey
		params, err := askActionParams(c, action, nil)
		if err != nil {
			return err
		}
		task["actionParameters"] = params
		if v, err := c.opt("custom-name", "Custom name of the action (optional)", ""); err != nil {
			return err
		} else if v != "" {
			task["customActionName"] = v
		}
	}

	// when: cron or a date
	cron := c.quiet("cron", "")
	dateFlag := c.quiet("date", "")
	if cron == "" && dateFlag == "" {
		def := "date"
		if action != nil {
			def = "cron"
		}
		when, err := c.enum("when", "Schedule", []string{"date", "cron"}, def)
		if err != nil {
			return err
		}
		if when == "cron" {
			cron, err = c.req("cron", "Cron expression (sec min hour day month weekday)", str(action, "defaultCronValue"))
			if err != nil {
				return err
			}
		}
	}
	if cron != "" {
		for !validCron(c, cron) {
			if !c.interactive || c.has("cron") {
				return fmt.Errorf("invalid cron expression %q", cron)
			}
			fmt.Fprintln(stdout, red("  invalid cron expression"))
			if cron, err = askRequired("Cron expression", cron); err != nil {
				return err
			}
		}
		task["cronExpression"] = cron
	} else {
		d, err := c.date("date", "When (YYYY-MM-DD HH:mm)", dateFlag, true)
		if err != nil {
			return err
		}
		task["specificDate"] = iso(*d)
	}
	for _, f := range []struct {
		flag, label, field string
		def                bool
	}{
		{"mail", "Send a mail?", "sendMail", action == nil},
		{"sms", "Send a sms?", "sendSms", false},
		{"in-app", "In-app notification?", "inAppNotification", true},
	} {
		v, err := c.boolOpt(f.flag, f.label, f.def)
		if err != nil {
			return err
		}
		task[f.field] = v
	}
	if action != nil {
		v, err := c.boolOpt("persist", "Keep the results?", true)
		if err != nil {
			return err
		}
		task["persistResult"] = v
	}
	if err := c.api.Post("/api/reminder-task/save", nil, task, nil); err != nil {
		return err
	}
	info("task %q saved", title)
	return nil
}

// askActionParams: --param key=value, else asked by type
func askActionParams(c *Ctx, action M, current []M) ([]M, error) {
	given := map[string]string{}
	for _, p := range c.flags("param") {
		k, v, _ := strings.Cut(p, "=")
		given[k] = v
	}
	cur := map[string]string{}
	for _, p := range current {
		cur[str(p, "key")] = str(p, "value")
	}
	var out []M
	for _, p := range maps(action["allowedParameters"]) {
		key := str(p, "key")
		param := M{}
		for k, v := range p {
			param[k] = v
		}
		val, ok := given[key]
		if !ok {
			def := cur[key]
			if def == "" {
				def = str(p, "value")
			}
			label := key
			if d := str(p, "description"); d != "" {
				label += dim(" (" + d + ")")
			}
			switch {
			case !c.interactive || c.yes:
				val = def
				if val == "" && boolean(p, "required") {
					return nil, fmt.Errorf("missing --param %s=...", key)
				}
			case str(p, "parameterType") == "OPTION":
				opts := get(p, "options")
				om, _ := opts.(map[string]any)
				keys := sortedKeys(om)
				labels := make([]string, len(keys))
				for i, k := range keys {
					labels[i] = fmt.Sprint(om[k]) + dim(" ("+k+")")
				}
				i, err := choose(label, labels)
				if err != nil {
					return nil, err
				}
				val = keys[i]
			case str(p, "parameterType") == "BOOLEAN":
				b, err := confirm(label, def == "true")
				if err != nil {
					return nil, err
				}
				val = fmt.Sprint(b)
			case str(p, "parameterType") == "BIG_STRING":
				v, err := c.longText("param-"+key, label, def, boolean(p, "required"))
				if err != nil {
					return nil, err
				}
				val = v
			default:
				var err error
				if boolean(p, "required") {
					val, err = askRequired(label, def)
				} else {
					val, err = ask(label, def)
				}
				if err != nil {
					return nil, err
				}
			}
		}
		param["value"] = val
		out = append(out, param)
	}
	return out, nil
}

func taskEdit(c *Ctx) error {
	t, err := fetchTask(c)
	if err != nil {
		return err
	}
	var edited M
	changed, err := editJSON(c.cfg, t, &edited)
	if err != nil {
		return err
	}
	if !changed {
		info("no change")
		return nil
	}
	if err := c.api.Post("/api/reminder-task/save", nil, edited, nil); err != nil {
		return err
	}
	info("task saved")
	return nil
}

func taskSetDisabled(c *Ctx, disabled bool) error {
	t, err := fetchTask(c)
	if err != nil {
		return err
	}
	t["disabled"] = disabled
	if err := c.api.Post("/api/reminder-task/save", nil, t, nil); err != nil {
		return err
	}
	info("%s %s", str(t, "title"), map[bool]string{true: "disabled", false: "enabled"}[disabled])
	return nil
}

func taskDelete(c *Ctx) error {
	id, err := c.pickID("task", func() ([]M, error) { return loadTasks(c) }, taskDesc)
	if err != nil {
		return err
	}
	if err := c.confirmAction("Delete this task?"); err != nil {
		return err
	}
	if err := c.api.Delete("/api/reminder-task", Q("id", id), nil); err != nil {
		return err
	}
	info("deleted")
	return nil
}

func taskActions(c *Ctx) error {
	l, err := loadActions(c)
	if err != nil {
		return err
	}
	sort.Slice(l, func(i, j int) bool { return str(l[i], "key") < str(l[j], "key") })
	return c.result(l, func() {
		for _, a := range l {
			fmt.Fprintf(c.out, "%s  %s\n  %s\n", bold(str(a, "key")), str(a, "title"), dim(str(a, "description")))
			if cron := str(a, "defaultCronValue"); cron != "" {
				fmt.Fprintf(c.out, "  default cron: %s\n", cron)
			}
			for _, p := range maps(a["allowedParameters"]) {
				req := ""
				if boolean(p, "required") {
					req = red("*")
				}
				fmt.Fprintf(c.out, "    --param %s%s=<%s> %s\n", str(p, "key"), req, strings.ToLower(str(p, "parameterType")), dim(str(p, "description")))
			}
			fmt.Fprintln(c.out)
		}
	})
}

func taskResults(c *Ctx) error {
	key, ok := c.flagOrPos("key")
	if !ok {
		actions, err := loadActions(c)
		if err != nil {
			return err
		}
		if !c.interactive {
			return missing("key")
		}
		opts := make([]string, len(actions))
		for i, a := range actions {
			opts[i] = str(a, "key")
		}
		i, err := choose("Action", opts)
		if err != nil {
			return err
		}
		key = opts[i]
	}
	page, size, err := c.pageParams(10)
	if err != nil {
		return err
	}
	var p M
	if err := c.api.Get("/api/reminder-task/action-results", Q("key", key, "page", page, "size", size, "sort", "createdDate,DESC"), &p); err != nil {
		return err
	}
	return c.result(p, func() {
		for _, r := range content(p) {
			status := str(r, "status")
			switch status {
			case "SUCCESS":
				status = green(status)
			case "FAILURE":
				status = red(status)
			}
			fmt.Fprintf(c.out, "%s %s → %s\n", status, fdatetime(r, "startedDate"), fdatetime(r, "finishedDate"))
			for _, m := range strList(r, "messages") {
				fmt.Fprintf(c.out, "  %s\n", strings.ReplaceAll(m, "\n", "\n  "))
			}
		}
		fmt.Fprintln(c.out, dim(pageInfo(p)))
	})
}

func taskValidateCron(c *Ctx) error {
	expr := strings.Join(c.restPos(), " ")
	if expr == "" {
		var err error
		if expr, err = c.req("cron", "Cron expression", ""); err != nil {
			return err
		}
	}
	if validCron(c, expr) {
		info("valid: %s", expr)
		return nil
	}
	return fmt.Errorf("invalid cron expression: %s", expr)
}
