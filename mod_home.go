package main

import (
	"fmt"
	"strings"
)

func init() {
	register(&Module{
		Name: "home", Aliases: []string{"dashboard"}, Title: "Home", Routes: []string{""},
		Help: "dashboard, todos, post-its, memo dates, notifications",
		Actions: []*Action{
			{Name: "dashboard", Aliases: []string{"show"}, Help: "the most useful figures: to invoice, revenue, hours, expenses, charts, tasks", Run: dashboard},
			{Name: "todos", Help: "list the todos", Usage: "[--all]", Run: todoList},
			{Name: "todo-add", Help: "add a todo", Usage: "<title>", Run: todoAdd},
			{Name: "todo-done", Help: "mark todos done / not done", Usage: "<id...>", Run: todoToggle},
			{Name: "todo-delete", Help: "delete todos", Usage: "<id...>", Run: todoDelete},
			{Name: "postits", Help: "list the post-its", Run: postitList},
			{Name: "postit-add", Help: "add a post-it", Usage: "<note>", Run: postitAdd},
			{Name: "postit-delete", Help: "delete post-its", Usage: "<id...>", Run: postitDelete},
			{Name: "memos", Help: "list the memo dates (days since...)", Run: memoList},
			{Name: "memo-add", Help: "add a memo date", Usage: "--label <label> --date <YYYY-MM-DD>", Run: memoAdd},
			{Name: "memo-delete", Help: "delete a memo date", Usage: "<id>", Run: memoDelete},
			notificationsAction("notifications"),
			notificationsSeenAction(),
			notificationsDeleteAction(),
		},
	})
}

func loadTodos(c *Ctx) ([]M, error) {
	var l []M
	return l, c.api.Get("/api/todo", nil, &l)
}

func todoList(c *Ctx) error {
	l, err := loadTodos(c)
	if err != nil {
		return err
	}
	return c.result(l, func() {
		rows := [][]string{}
		for _, t := range l {
			if boolean(t, "done") && !c.boolFlag("all") {
				continue
			}
			box := "☐"
			if boolean(t, "done") {
				box = green("☑")
			}
			rows = append(rows, []string{box, str(t, "title"), dim(str(t, "id"))})
		}
		table(c.out, []string{"", "todo", "id"}, rows)
	})
}

func todoAdd(c *Ctx) error {
	title := strings.Join(c.restPos(), " ")
	if title == "" {
		var err error
		if title, err = c.req("title", "Todo", ""); err != nil {
			return err
		}
	}
	var saved M
	if err := c.api.Post("/api/todo", nil, M{"title": title, "done": false}, &saved); err != nil {
		return err
	}
	return c.result(saved, func() { info("todo added (%s)", str(saved, "id")) })
}

func todoDesc(t M) string {
	box := "☐ "
	if boolean(t, "done") {
		box = "☑ "
	}
	return box + str(t, "title")
}

func todoToggle(c *Ctx) error {
	todos, err := loadTodos(c)
	if err != nil {
		return err
	}
	ids, err := c.pickMany("todos", func() ([]M, error) { return todos, nil }, todoDesc)
	if err != nil {
		return err
	}
	for _, id := range ids {
		for _, t := range todos {
			if str(t, "id") == id {
				t["done"] = !boolean(t, "done")
				if err := c.api.Post("/api/todo", nil, t, nil); err != nil {
					return err
				}
				info("%s", todoDesc(t))
			}
		}
	}
	return nil
}

func todoDelete(c *Ctx) error {
	ids, err := c.pickMany("todos", func() ([]M, error) { return loadTodos(c) }, todoDesc)
	if err != nil {
		return err
	}
	if err := c.confirmAction(fmt.Sprintf("Delete %d todo(s)?", len(ids))); err != nil {
		return err
	}
	for _, id := range ids {
		if err := c.api.Delete("/api/todo", Q("id", id), nil); err != nil {
			return err
		}
	}
	info("deleted")
	return nil
}

func loadPostits(c *Ctx) ([]M, error) {
	var l []M
	return l, c.api.Get("/api/post-it", nil, &l)
}

func postitList(c *Ctx) error {
	l, err := loadPostits(c)
	if err != nil {
		return err
	}
	return c.result(l, func() {
		for _, p := range l {
			fmt.Fprintf(c.out, "%s %s\n  %s\n", yellow("■"), dim(fdatetime(p, "creationDate")+"  "+str(p, "id")), strings.ReplaceAll(str(p, "note"), "\n", "\n  "))
		}
		if len(l) == 0 {
			fmt.Fprintln(c.out, dim("(no post-it)"))
		}
	})
}

func postitAdd(c *Ctx) error {
	note := strings.Join(c.restPos(), " ")
	if note == "" {
		var err error
		if note, err = c.longText("note", "Note", "", true); err != nil {
			return err
		}
	}
	var saved M
	if err := c.api.Post("/api/post-it", nil, M{"note": note}, &saved); err != nil {
		return err
	}
	return c.result(saved, func() { info("post-it added") })
}

func postitDelete(c *Ctx) error {
	ids, err := c.pickMany("post-its", func() ([]M, error) { return loadPostits(c) }, func(p M) string { return truncate(str(p, "note"), 70) })
	if err != nil {
		return err
	}
	if err := c.confirmAction(fmt.Sprintf("Delete %d post-it(s)?", len(ids))); err != nil {
		return err
	}
	for _, id := range ids {
		if err := c.api.Delete("/api/post-it", Q("id", id), nil); err != nil {
			return err
		}
	}
	info("deleted")
	return nil
}

func loadMemos(c *Ctx) ([]M, error) {
	var l []M
	return l, c.api.Post("/api/memo-date", nil, nil, &l)
}

func memoList(c *Ctx) error {
	l, err := loadMemos(c)
	if err != nil {
		return err
	}
	return c.result(l, func() {
		rows := [][]string{}
		for _, m := range l {
			days := ""
			if t, ok := toTime(get(m, "dateSince")); ok {
				days = fmt.Sprintf("%d days", int(timeSinceDays(t)))
			}
			rows = append(rows, []string{str(m, "label"), fdate(m, "dateSince"), days, dim(str(m, "id"))})
		}
		table(c.out, []string{"label", "since", "", "id"}, rows)
	})
}

func memoAdd(c *Ctx) error {
	label, err := c.reqPos("label", "Label", "")
	if err != nil {
		return err
	}
	d, err := c.date("date", "Since", "today", true)
	if err != nil {
		return err
	}
	var saved M
	if err := c.api.Post("/api/memo-date/save", nil, M{"label": label, "dateSince": iso(*d)}, &saved); err != nil {
		return err
	}
	return c.result(saved, func() { info("memo date added") })
}

func memoDelete(c *Ctx) error {
	id, err := c.pickID("memo date", func() ([]M, error) { return loadMemos(c) }, func(m M) string { return str(m, "label") + " " + dim(fdate(m, "dateSince")) })
	if err != nil {
		return err
	}
	if err := c.confirmAction("Delete this memo date?"); err != nil {
		return err
	}
	if err := c.api.Delete("/api/memo-date", Q("id", id), nil); err != nil {
		return err
	}
	info("deleted")
	return nil
}

// ---------- notifications (home and admin) ----------

func loadNotifications(c *Ctx) ([]M, error) {
	var l []M
	return l, c.api.Get("/api/notification", nil, &l)
}

func notifDesc(n M) string {
	s := fdatetime(n, "receivedDate") + "  " + str(n, "title")
	if !boolean(n, "seen") {
		s = bold("● ") + s
	}
	return s
}

func notificationsAction(name string) *Action {
	return &Action{Name: name, Aliases: []string{"notifs"}, Help: "latest notifications", Usage: "[--unseen]", Run: func(c *Ctx) error {
		l, err := loadNotifications(c)
		if err != nil {
			return err
		}
		return c.result(l, func() {
			rows := [][]string{}
			for _, n := range l {
				if c.boolFlag("unseen") && boolean(n, "seen") {
					continue
				}
				rows = append(rows, []string{mark(!boolean(n, "seen"), bold("●")), fdatetime(n, "receivedDate"), str(n, "title"), str(n, "type"), dim(str(n, "id"))})
			}
			table(c.out, []string{"", "received", "title", "type", "id"}, rows)
		})
	}}
}

func notificationsSeenAction() *Action {
	return &Action{Name: "notifications-seen", Aliases: []string{"seen"}, Help: "mark notifications as seen (--all, or --unseen=false to mark unseen)", Usage: "<id...> | --all", Run: func(c *Ctx) error {
		seen := true
		if v, ok := c.flag("seen"); ok {
			seen, _ = parseBool(v)
		}
		var ids []string
		if c.boolFlag("all") {
			l, err := loadNotifications(c)
			if err != nil {
				return err
			}
			for _, n := range l {
				if boolean(n, "seen") != seen {
					ids = append(ids, str(n, "id"))
				}
			}
		} else {
			var err error
			ids, err = c.pickMany("notifications", func() ([]M, error) { return loadNotifications(c) }, notifDesc)
			if err != nil {
				return err
			}
		}
		for _, id := range ids {
			if err := c.api.Post("/api/notification", Q("id", id, "seen", seen), nil, nil); err != nil {
				return err
			}
		}
		info("%d notification(s) updated", len(ids))
		return nil
	}}
}

func notificationsDeleteAction() *Action {
	return &Action{Name: "notifications-delete", Help: "delete notifications (--all: the seen ones)", Usage: "<id...> | --all", Run: func(c *Ctx) error {
		var ids []string
		if c.boolFlag("all") {
			l, err := loadNotifications(c)
			if err != nil {
				return err
			}
			for _, n := range l {
				if boolean(n, "seen") {
					ids = append(ids, str(n, "id"))
				}
			}
		} else {
			var err error
			if ids, err = c.pickMany("notifications", func() ([]M, error) { return loadNotifications(c) }, notifDesc); err != nil {
				return err
			}
		}
		if len(ids) == 0 {
			info("nothing to delete")
			return nil
		}
		if err := c.confirmAction(fmt.Sprintf("Delete %d notification(s)?", len(ids))); err != nil {
			return err
		}
		for _, id := range ids {
			if err := c.api.Delete("/api/notification", Q("id", id), nil); err != nil {
				return err
			}
		}
		info("deleted")
		return nil
	}}
}
