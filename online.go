package main

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// --online: opens the page of the backoffice showing what was asked, instead of printing it.

// the page of each module in the backoffice (routes.tsx)
var modulePages = map[string]string{
	"home": "/", "stats": "/", "tasks": "/tasks", "timesheets": "/timesheets", "expenses": "/fee/unprocessed",
	"invoices": "/invoice/unprocessed", "dossiers": "/dossier/open", "documents": "/documents/home", "reports": "/report",
	"gallery": "/memzagram", "cv": "/cv", "finance": "/finance/portfolio", "uploads": "/file-upload/home",
	"sparql": "/endpoint-sparql", "prospects": "/services", "clients": "/billable-clients", "mails": "/mails",
	"templates": "/templates", "admin": "/administration", "toolbox": "/administration/toolbox",
}

// the actions showing something (list / show pages) and their page, when it is not the one of the module
var onlinePages = map[string]func(c *Ctx) (string, error){
	"expenses list": func(c *Ctx) (string, error) {
		switch {
		case c.boolFlag("bookmarked"):
			return "/fee/bookmarks", nil
		case c.boolFlag("processed") || c.boolFlag("archived"):
			return "/fee/processed", nil
		}
		return "/fee/unprocessed", nil
	},
	"expenses exceptional": static("/fee/exceptional"),
	"invoices list": func(c *Ctx) (string, error) {
		switch {
		case c.boolFlag("bookmarked"):
			return "/invoice/bookmarked", nil
		case c.boolFlag("deleted"):
			return "/invoice/deleted-logically", nil
		case c.boolFlag("archived"):
			return "/invoice/processed", nil
		}
		return "/invoice/unprocessed", nil
	},
	"dossiers list": func(c *Ctx) (string, error) {
		switch {
		case c.boolFlag("bookmarked"):
			return "/dossier/bookmarks", nil
		case c.boolFlag("closed"):
			return "/dossier/closed", nil
		}
		return "/dossier/open", nil
	},
	"documents list": func(c *Ctx) (string, error) {
		if c.boolFlag("bookmarked") {
			return "/documents/bookmarks", nil
		}
		return "/documents/home", nil
	},
	"uploads list": func(c *Ctx) (string, error) {
		if c.boolFlag("bookmarked") {
			return "/file-upload/bookmarks", nil
		}
		return "/file-upload/home", nil
	},
	"uploads search": static("/file-upload/fuzzy-search"),
	"reports list": func(c *Ctx) (string, error) {
		tabs := map[string]string{"DRAFT": "drafts", "IN_PROGRESS": "in-progress", "PENDING": "pending", "DONE": "done"}
		if t, ok := tabs[strings.ToUpper(c.quiet("status", ""))]; ok {
			return "/report/" + t, nil
		}
		return "/report", nil
	},
	"reports show": func(c *Ctx) (string, error) {
		p, err := pickPost(c)
		if err != nil {
			return "", err
		}
		slug := slugify(str(p, "title"))
		if slug == "" {
			slug = "post"
		}
		return "/report/post/" + slug + "/" + str(p, "id"), nil
	},
	"timesheets show": func(c *Ctx) (string, error) {
		id, err := c.pickID("timesheet", func() ([]M, error) { return loadTimesheets(c) }, tsDesc)
		if err != nil {
			return "", err
		}
		return "/timesheets/timesheet/" + id, nil
	},
	"cv requests":   static("/cv/download-requests"),
	"admin logs":    static("/administration/logs"),
	"admin scripts": static("/administration/scripts"),
	"admin info":    static("/administration"),
	"templates list": func(c *Ctx) (string, error) {
		if t := c.quiet("type", ""); t != "" {
			return "/templates?type=" + url.QueryEscape(strings.ToUpper(t)), nil
		}
		return "/templates", nil
	},
	"finance list": static("/finance/portfolio"),
	"finance show": static("/finance/portfolio"),
}

func static(p string) func(*Ctx) (string, error) { return func(*Ctx) (string, error) { return p, nil } }

// onlinePage: the page of an action, ok false when it shows nothing of its own (an update, an upload...)
func onlinePage(m *Module, a *Action) (func(c *Ctx) (string, error), bool) {
	if f, ok := onlinePages[docKey(m, a)]; ok {
		return f, true
	}
	page, ok := modulePages[m.Name]
	if !ok {
		return nil, false
	}
	// the list / show actions, the dashboards and every chart
	switch {
	case m.Name == "stats",
		a.Name == "list", a.Name == "show", a.Name == "dashboard", a.Name == "active", a.Name == "summaries":
		return static(page), true
	}
	return nil, false
}

var slugRe1, slugRe2 = regexp.MustCompile(`[^\w-]+`), regexp.MustCompile(`--+`)

// slugify like the backoffice (lib/format.ts)
func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.Join(strings.Fields(s), "-")
	s = slugRe1.ReplaceAllString(s, "")
	s = slugRe2.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

func openOnline(c *Ctx, m *Module, a *Action) error {
	f, ok := onlinePage(m, a)
	if !ok {
		return fmt.Errorf("--online: %s %s has no page of its own (the list and show actions do)", m.Name, a.Name)
	}
	base := strings.TrimRight(c.cfg.FrontendURL, "/")
	if base == "" {
		return fmt.Errorf("--online needs the url of the backoffice: artcoded config set frontend_url https://backoffice.example.org")
	}
	p, err := f(c)
	if err != nil {
		return err
	}
	target := base + p
	if err := openInBrowser(target); err != nil {
		fmt.Fprintln(stdout, target)
		return nil
	}
	info("opened %s", target)
	return nil
}
