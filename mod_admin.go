package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func init() {
	register(&Module{
		Name: "mails", Aliases: []string{"mail"}, Title: "Mails", Routes: []string{"mails"},
		Help: "send mails with attachments, see the queue",
		Actions: []*Action{
			{Name: "list", Aliases: []string{"ls"}, Help: "mails sent and scheduled", Usage: "[--page n]", Run: mailList},
			{Name: "show", Help: "a mail", Usage: "<id>", Run: mailShow},
			{Name: "send", Aliases: []string{"new", "compose"}, Help: "send a mail (files from paths or upload ids)", Usage: "--to a@b.c[,..] --subject .. [--body ..|--body-file f] [--file path]... [--upload-id id]... [--bcc --at date]", Run: mailSend},
			{Name: "retry", Help: "send a failed mail again", Usage: "<id>", Run: mailRetry},
			{Name: "edit", Help: "edit a scheduled mail (json in the editor)", Usage: "<id>", Run: mailEdit},
			{Name: "delete", Aliases: []string{"rm"}, Help: "delete a mail", Usage: "<id>", Run: mailDelete},
		},
	})
	register(&Module{
		Name: "admin", Aliases: []string{"administration", "settings"}, Title: "Administration", Routes: []string{"administration"},
		Help: "company info, menu, caches, scripts, sms, backend health and logs",
		Actions: []*Action{
			{Name: "info", Aliases: []string{"health"}, Help: "backend version and health", Run: adminInfo},
			{Name: "logs", Help: "backend log file", Usage: "[--tail 200 --grep text]", Run: adminLogs},
			{Name: "company", Aliases: []string{"personal-info"}, Help: "company / personal info", Run: companyShow},
			{Name: "company-edit", Help: "edit the company info (logo, signature, accountants...)", Usage: "[--logo img --signature img --initial img --<field> value]", Run: companyEdit},
			{Name: "menu", Help: "the menu of the backoffice (and of this cli)", Run: menuList},
			{Name: "menu-toggle", Help: "show / hide menu entries", Usage: "<id...>", Run: menuToggle},
			{Name: "menu-edit", Help: "edit the whole menu (json in the editor)", Run: menuEdit},
			{Name: "caches", Help: "caches of the backend", Run: cacheList},
			{Name: "cache-clear", Help: "clear caches", Usage: "<name...> | --all", Run: cacheClear},
			{Name: "scripts", Help: "scripts loaded in the backend", Run: scriptList},
			{Name: "script-run", Help: "run a script (file, argument or editor)", Usage: "[--file script.js]", Run: scriptRun},
			{Name: "user-scripts", Help: "user scripts", Run: userScriptList},
			{Name: "user-script-save", Help: "add / edit a user script (editor)", Usage: "[<id>] [--file script.js]", Run: userScriptSave},
			{Name: "user-script-delete", Help: "delete a user script", Usage: "<id>", Run: userScriptDelete},
			{Name: "script-bindings", Help: "what a script can use", Run: func(c *Ctx) error {
				var m any
				if err := c.api.Post("/api/script/bindings", nil, nil, &m); err != nil {
					return err
				}
				return c.printJSON(m)
			}},
			{Name: "sms", Help: "send a sms", Usage: "--to +32... --message ..", Run: smsSend},
			{Name: "dumps", Help: "database dumps", Usage: "[--snapshot]", Run: dumpList},
			notificationsAction("notifications"),
			notificationsSeenAction(),
			notificationsDeleteAction(),
		},
	})
	register(&Module{
		Name: "toolbox", Aliases: []string{"tools"}, Title: "Toolbox", Routes: []string{"administration"},
		Help: "pdf split / rotate, rdf conversion, shacl, xpath",
		Actions: []*Action{
			{Name: "pdf-split", Help: "split a pdf in pages (zip)", Usage: "<pdf> [--out path]", Run: toolPdfSplit},
			{Name: "pdf-rotate", Help: "rotate an uploaded pdf", Usage: "<upload id> [--rotation 90|180|270]", Run: toolPdfRotate},
			{Name: "rdf-convert", Help: "convert an rdf file to another syntax", Usage: "<file> --lang TURTLE", Run: toolRdfConvert},
			{Name: "rdf-langs", Help: "rdf syntaxes", Run: func(c *Ctx) error {
				var l []string
				if err := c.api.Get("/api/toolbox/public/rdf/allowed-languages", nil, &l); err != nil {
					return err
				}
				return c.result(l, func() { fmt.Fprintln(c.out, strings.Join(l, "\n")) })
			}},
			{Name: "shacl", Help: "validate an rdf model with shacl shapes", Usage: "<model> <shapes>", Run: toolShacl},
			{Name: "xpath", Help: "evaluate an xpath expression on an xml file", Usage: "<expression> <xml file>", Run: toolXpath},
		},
	})
}

// ---------- mails ----------

func loadMails(c *Ctx, page, size int) (M, error) {
	var p M
	return p, c.api.Get("/api/mail/find-all", Q("page", page, "size", size, "sort", "creationDate,DESC"), &p)
}

func mailState(m M) string {
	switch {
	case boolean(m, "markedFailed"):
		return red("failed")
	case boolean(m, "sent"):
		return green("sent")
	default:
		return yellow("scheduled")
	}
}

func mailDesc(m M) string {
	return fmt.Sprintf("%s  %s  %s  %s", fdatetime(m, "sendingDate"), str(m, "to"), truncate(str(m, "subject"), 40), mailState(m))
}

func mailList(c *Ctx) error {
	page, size, err := c.pageParams(20)
	if err != nil {
		return err
	}
	p, err := loadMails(c, page, size)
	if err != nil {
		return err
	}
	return c.result(p, func() {
		rows := [][]string{}
		for _, m := range content(p) {
			rows = append(rows, []string{fdatetime(m, "sendingDate"), str(m, "to"), str(m, "subject"), fmt.Sprint(len(list(m, "uploadIds"))), mailState(m), dim(str(m, "id"))})
		}
		table(c.out, []string{"sending", "to", "subject", "files", "", "id"}, rows)
		fmt.Fprintln(c.out, dim(pageInfo(p)))
	})
}

func pickMail(c *Ctx) (M, error) {
	all := func() ([]M, error) {
		p, err := loadMails(c, 0, 100)
		return content(p), err
	}
	m, err := c.pick("mail", all, mailDesc)
	if err != nil || len(m) > 1 {
		return m, err
	}
	l, err := all()
	if err != nil {
		return nil, err
	}
	for _, x := range l {
		if str(x, "id") == str(m, "id") {
			return x, nil
		}
	}
	return nil, fmt.Errorf("mail %s not in the 100 latest", str(m, "id"))
}

func mailShow(c *Ctx) error {
	m, err := pickMail(c)
	if err != nil {
		return err
	}
	return c.result(m, func() {
		details(c.out, "subject", bold(str(m, "subject")), "to", str(m, "to"), "bcc", mark(boolean(m, "bcc"), "yes"), "state", mailState(m),
			"sending", fdatetime(m, "sendingDate"), "error", str(m, "markedFailedMessage"), "id", str(m, "id"))
		fmt.Fprintln(c.out, "\n"+str(m, "body"))
		printUploads(c, "Files", strList(m, "uploadIds"))
	})
}

func mailSend(c *Ctx) error {
	to, err := c.req("to", "To (comma separated)", "")
	if err != nil {
		return err
	}
	subject, err := c.req("subject", "Subject", "")
	if err != nil {
		return err
	}
	body, err := c.longText("body", "Body", "", true)
	if err != nil {
		return err
	}
	ids := append([]string{}, c.flags("upload-id")...)
	paths, err := c.files("file", "Attachment", false)
	if err != nil {
		return err
	}
	for _, p := range paths {
		u, err := c.api.Upload(p, "", false)
		if err != nil {
			return err
		}
		ids = append(ids, str(u, "id"))
		info("%s uploaded", filepath.Base(p))
	}
	req := M{"to": splitTags(to), "subject": subject, "body": body, "bcc": c.boolFlag("bcc"), "uploadIds": ids}
	if at, ok := c.flag("at"); ok {
		t, err := parseDate(at)
		if err != nil {
			return err
		}
		req["sendingDate"] = iso(t)
	}
	if err := c.confirmAction(fmt.Sprintf("Send %q to %s with %d file(s)?", subject, to, len(ids))); err != nil {
		return err
	}
	if err := c.api.Post("/api/mail/send", nil, req, nil); err != nil {
		return err
	}
	info("mail queued")
	return nil
}

func mailRetry(c *Ctx) error {
	m, err := pickMail(c)
	if err != nil {
		return err
	}
	if err := c.api.Post("/api/mail/retry", nil, m, nil); err != nil {
		return err
	}
	info("mail queued again")
	return nil
}

func mailEdit(c *Ctx) error {
	m, err := pickMail(c)
	if err != nil {
		return err
	}
	var edited M
	changed, err := editJSON(c.cfg, m, &edited)
	if err != nil || !changed {
		if err == nil {
			info("no change")
		}
		return err
	}
	if err := c.api.Post("/api/mail/update", nil, edited, nil); err != nil {
		return err
	}
	info("mail saved")
	return nil
}

func mailDelete(c *Ctx) error {
	m, err := pickMail(c)
	if err != nil {
		return err
	}
	if err := c.confirmAction("Delete the mail " + str(m, "subject") + "?"); err != nil {
		return err
	}
	if err := c.api.Delete("/api/mail/delete", Q("id", str(m, "id")), nil); err != nil {
		return err
	}
	info("deleted")
	return nil
}

// ---------- admin ----------

func adminInfo(c *Ctx) error {
	var bi, health M
	_ = c.api.Get("/api/actuator/info", nil, &bi)
	herr := c.api.Get("/api/actuator/health", nil, &health)
	if health == nil && herr != nil {
		// spring answers 503 with the health body when something is down
		health = M{"status": "DOWN", "error": herr.Error()}
	}
	return c.result(M{"info": bi, "health": health}, func() {
		details(c.out, "backend", c.cfg.BackendURL, "version", str(bi, "build.version"), "built", fdatetime(bi, "build.time"), "status", statusColor(str(health, "status")))
		if comps, ok := health["components"].(map[string]any); ok {
			for _, k := range sortedKeys(comps) {
				extra := ""
				if k == "diskSpace" {
					extra = dim(fmt.Sprintf(" %s free of %s", humanSize(num(comps[k], "details.free")), humanSize(num(comps[k], "details.total"))))
				}
				fmt.Fprintf(c.out, "  %-12s %s%s\n", k, statusColor(str(comps[k], "status")), extra)
			}
		}
		fmt.Fprintln(c.out, dim("cli "+version))
	})
}

func statusColor(s string) string {
	switch s {
	case "UP":
		return green(s)
	case "":
		return ""
	default:
		return red(s)
	}
}

func adminLogs(c *Ctx) error {
	text, err := c.api.FetchText(http.MethodGet, "/api/actuator/logfile", nil, nil, "")
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if g, ok := c.flag("grep"); ok {
		var f []string
		for _, l := range lines {
			if strings.Contains(strings.ToLower(l), strings.ToLower(g)) {
				f = append(f, l)
			}
		}
		lines = f
	}
	tail, err := c.intFlag("tail", 200)
	if err != nil {
		return err
	}
	if tail > 0 && len(lines) > tail {
		lines = lines[len(lines)-tail:]
	}
	fmt.Fprintln(c.out, strings.Join(lines, "\n"))
	return nil
}

func companyShow(c *Ctx) error {
	var p M
	if err := c.api.Get("/api/personal-info", nil, &p); err != nil {
		return err
	}
	return c.result(p, func() {
		details(c.out, "organization", bold(str(p, "organizationName")), "ceo", str(p, "ceoFullName"), "vat number", str(p, "vatNumber"),
			"address", str(p, "organizationAddress"), "city", str(p, "organizationPostCode")+" "+str(p, "organizationCity"), "country", str(p, "countryCode"),
			"email", str(p, "organizationEmailAddress"), "phone", str(p, "organizationPhoneNumber"), "phone 2", str(p, "organizationSecondPhoneNumber"),
			"bank account", str(p, "organizationBankAccount"), "bic", str(p, "organizationBankBIC"), "days to pay", str(p, "maxDaysToPay"),
			"finance charge", str(p, "financeCharge"), "demo mode", mark(boolean(p, "demoMode"), yellow("on")), "note", str(p, "note"))
		for _, a := range maps(p["accountants"]) {
			fmt.Fprintf(c.out, "  accountant %s %s <%s> %s\n", str(a, "firstName"), str(a, "lastName"), str(a, "email"), str(a, "phoneNumber"))
		}
	})
}

func companyEdit(c *Ctx) error {
	var p M
	if err := c.api.Get("/api/personal-info", nil, &p); err != nil {
		return err
	}
	fields := []struct{ field, label string }{
		{"organizationName", "Organization name"}, {"ceoFullName", "CEO full name"}, {"vatNumber", "VAT number"},
		{"organizationAddress", "Address"}, {"organizationPostCode", "Post code"}, {"organizationCity", "City"}, {"countryCode", "Country code"},
		{"organizationEmailAddress", "Email"}, {"organizationPhoneNumber", "Phone"}, {"organizationSecondPhoneNumber", "Second phone (optional)"},
		{"organizationBankAccount", "Bank account (IBAN)"}, {"organizationBankBIC", "BIC"}, {"maxDaysToPay", "Days to pay"},
		{"financeCharge", "Finance charge (%)"}, {"note", "Note (invoices)"},
	}
	parts := []Part{}
	for _, f := range fields {
		flag := strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(f.field, "organization"), "_", "-"))
		v, err := c.opt(flag, f.label, str(p, f.field))
		if err != nil {
			return err
		}
		parts = append(parts, F(f.field, v))
	}
	demo, err := c.boolOpt("demo", "Demo mode?", boolean(p, "demoMode"))
	if err != nil {
		return err
	}
	parts = append(parts, F("demoMode", fmt.Sprint(demo)))
	accountants := p["accountants"]
	if accountants == nil {
		accountants = []any{}
	}
	if c.interactive && !c.yes && askBool("Edit the accountants (json in the editor)?", false) {
		var edited any
		if changed, err := editJSON(c.cfg, accountants, &edited); err != nil {
			return err
		} else if changed {
			accountants = edited
		}
	}
	acc, _ := json.Marshal(accountants)
	parts = append(parts, F("accountants", string(acc)))
	for _, img := range []string{"logo", "signature", "initial"} {
		v := c.quiet(img, "")
		if v == "" && c.interactive && !c.yes {
			if v, err = ask("New "+img+" image (path, empty to keep)", ""); err != nil {
				return err
			}
		}
		if v != "" {
			parts = append(parts, FFile(img, expandPath(v)))
		}
	}
	if err := c.api.Multipart("/api/personal-info/submit", nil, parts, nil); err != nil {
		return err
	}
	info("company info saved")
	return nil
}

func loadMenu(c *Ctx) ([]M, error) {
	var l []M
	return l, c.api.Get("/api/settings/menu-link", nil, &l)
}

func menuDesc(l M) string {
	return mark(!boolean(l, "show"), dim("(hidden) ")) + str(l, "title") + dim("  /"+strings.Join(strList(l, "routerLink"), "/"))
}

func menuList(c *Ctx) error {
	l, err := loadMenu(c)
	if err != nil {
		return err
	}
	return c.result(l, func() {
		rows := [][]string{}
		for _, x := range l {
			cli := ""
			route := strings.Join(strList(x, "routerLink"), "/")
			for _, m := range modules {
				for _, r := range m.Routes {
					if r == route {
						cli = m.Name
					}
				}
			}
			rows = append(rows, []string{str(x, "order"), str(x, "title"), "/" + route, yesNo(boolean(x, "show")), str(x, "roles"), cli, dim(str(x, "id"))})
		}
		table(c.out, []string{"order", "title", "route", "shown", "roles", "cli module", "id"}, rows)
	})
}

func menuToggle(c *Ctx) error {
	all, err := loadMenu(c)
	if err != nil {
		return err
	}
	ids, err := c.pickMany("menu entries", func() ([]M, error) { return all, nil }, menuDesc)
	if err != nil {
		return err
	}
	for _, id := range ids {
		for _, l := range all {
			if str(l, "id") == id {
				l["show"] = !boolean(l, "show")
				if err := c.api.Post("/api/settings/menu-link/save", nil, l, nil); err != nil {
					return err
				}
				info("%s: %s", str(l, "title"), map[bool]string{true: "shown", false: "hidden"}[boolean(l, "show")])
			}
		}
	}
	return nil
}

func menuEdit(c *Ctx) error {
	l, err := loadMenu(c)
	if err != nil {
		return err
	}
	var edited []M
	changed, err := editJSON(c.cfg, l, &edited)
	if err != nil || !changed {
		if err == nil {
			info("no change")
		}
		return err
	}
	if err := c.confirmAction("Replace the whole menu (the backend logs a backup)?"); err != nil {
		return err
	}
	if err := c.api.Post("/api/settings/menu-link/import", nil, edited, nil); err != nil {
		return err
	}
	info("menu saved")
	return nil
}

func cacheList(c *Ctx) error {
	var l []string
	if err := c.api.Post("/api/cache/find-all", nil, nil, &l); err != nil {
		return err
	}
	return c.result(l, func() { fmt.Fprintln(c.out, strings.Join(l, "\n")) })
}

func cacheClear(c *Ctx) error {
	var names []string
	if c.boolFlag("all") {
		if err := c.api.Post("/api/cache/find-all", nil, nil, &names); err != nil {
			return err
		}
	} else {
		names = c.restPos()
		if len(names) == 0 {
			var all []string
			if err := c.api.Post("/api/cache/find-all", nil, nil, &all); err != nil {
				return err
			}
			if !c.interactive {
				return missing("all")
			}
			sel, err := chooseMany("Caches to clear", all)
			if err != nil {
				return err
			}
			for _, i := range sel {
				names = append(names, all[i])
			}
		}
	}
	for _, n := range names {
		if err := c.api.Delete("/api/cache/clear", Q("name", n), nil); err != nil {
			return err
		}
	}
	info("%d cache(s) cleared", len(names))
	return nil
}

func scriptList(c *Ctx) error {
	var l []M
	if err := c.api.Post("/api/script", nil, nil, &l); err != nil {
		return err
	}
	return c.result(l, func() {
		rows := [][]string{}
		for _, s := range l {
			rows = append(rows, []string{str(s, "name"), yesNo(boolean(s, "enabled")), mark(boolean(s, "consumeEvent"), "events"), str(s, "description")})
		}
		table(c.out, []string{"script", "enabled", "", "description"}, rows)
	})
}

func scriptSource(c *Ctx, def string) (string, error) {
	if f, ok := c.flag("file"); ok {
		data, err := os.ReadFile(expandPath(f))
		return string(data), err
	}
	if a := strings.Join(c.restPos(), " "); a != "" {
		return a, nil
	}
	return editText(c.cfg, def, ".js")
}

func scriptRun(c *Ctx) error {
	src, err := scriptSource(c, "// the result of the last expression is printed\n")
	if err != nil {
		return err
	}
	out, err := c.api.FetchText(http.MethodPost, "/api/script/run", nil, mustJSON(M{"script": src}), "application/json")
	if err != nil {
		return err
	}
	fmt.Fprintln(c.out, out)
	return nil
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

func loadUserScripts(c *Ctx) ([]M, error) {
	var l []M
	return l, c.api.Get("/api/script/user-script", nil, &l)
}

func userScriptDesc(s M) string {
	first := strings.SplitN(strings.TrimSpace(str(s, "content")), "\n", 2)[0]
	return truncate(first, 60) + dim("  "+fdate(s, "updatedDate"))
}

func userScriptList(c *Ctx) error {
	l, err := loadUserScripts(c)
	if err != nil {
		return err
	}
	return c.result(l, func() {
		rows := [][]string{}
		for _, s := range l {
			rows = append(rows, []string{userScriptDesc(s), strconv.Itoa(len(str(s, "content"))) + " chars", dim(str(s, "id"))})
		}
		table(c.out, []string{"script", "size", "id"}, rows)
	})
}

func userScriptSave(c *Ctx) error {
	s := M{}
	if id, ok := c.flagOrPos("id"); ok {
		l, err := loadUserScripts(c)
		if err != nil {
			return err
		}
		for _, x := range l {
			if str(x, "id") == id {
				s = x
			}
		}
		if len(s) == 0 {
			return fmt.Errorf("user script %s not found", id)
		}
	} else if c.interactive && !c.has("file") {
		l, err := loadUserScripts(c)
		if err != nil {
			return err
		}
		opts := []string{"(new script)"}
		for _, x := range l {
			opts = append(opts, userScriptDesc(x))
		}
		i, err := choose("Script", opts)
		if err != nil {
			return err
		}
		if i > 0 {
			s = l[i-1]
		}
	}
	src, err := scriptSource(c, str(s, "content"))
	if err != nil {
		return err
	}
	s["content"] = src
	var saved M
	if err := c.api.Post("/api/script/user-script", nil, s, &saved); err != nil {
		return err
	}
	info("user script saved (%s)", str(saved, "id"))
	return nil
}

func userScriptDelete(c *Ctx) error {
	id, err := c.pickID("user script", func() ([]M, error) { return loadUserScripts(c) }, userScriptDesc)
	if err != nil {
		return err
	}
	if err := c.confirmAction("Delete this user script?"); err != nil {
		return err
	}
	if err := c.api.Delete("/api/script/user-script", Q("id", id), nil); err != nil {
		return err
	}
	info("deleted")
	return nil
}

func smsSend(c *Ctx) error {
	to, err := c.req("to", "Phone number", "")
	if err != nil {
		return err
	}
	msg, err := c.req("message", "Message", "")
	if err != nil {
		return err
	}
	if err := c.api.Post("/api/sms", nil, M{"phoneNumber": to, "message": msg}, nil); err != nil {
		return err
	}
	info("sms sent")
	return nil
}

func dumpList(c *Ctx) error {
	var l []string
	if err := c.api.Get("/api/mongo-management", Q("snapshot", c.boolFlag("snapshot")), &l); err != nil {
		return err
	}
	return c.result(l, func() { fmt.Fprintln(c.out, strings.Join(l, "\n")) })
}

// ---------- toolbox ----------

func toolPdfSplit(c *Ctx) error {
	path, err := c.file("file", "Pdf")
	if err != nil {
		return err
	}
	d, err := c.api.MultipartFetch("/api/pdf/split", []Part{FFile("pdf", path)}, strings.TrimSuffix(filepath.Base(path), ".pdf")+"-pages.zip")
	if err != nil {
		return err
	}
	return c.save(d)
}

func toolPdfRotate(c *Ctx) error {
	id, err := c.reqPos("id", "Upload id of the pdf", "")
	if err != nil {
		return err
	}
	rot, err := c.enum("rotation", "Rotation", []string{"90", "180", "270"}, "180")
	if err != nil {
		return err
	}
	if err := c.api.Post("/api/pdf/rotate", Q("id", id, "rotation", rot), nil, nil); err != nil {
		return err
	}
	info("rotated")
	return nil
}

func toolRdfConvert(c *Ctx) error {
	path, err := c.file("file", "Rdf file")
	if err != nil {
		return err
	}
	var langs []string
	_ = c.api.Get("/api/toolbox/public/rdf/allowed-languages", nil, &langs)
	lang := c.quiet("lang", "")
	if lang == "" {
		if lang, err = c.enum("lang", "Convert to", langs, "TURTLE"); err != nil {
			return err
		}
	}
	d, err := c.api.MultipartFetch("/api/toolbox/public/rdf/file-to-lang", []Part{FFile("file", path), F("lang", lang)},
		strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))+"."+strings.ToLower(strings.ReplaceAll(lang, "_", ".")))
	if err != nil {
		return err
	}
	if !c.has("out") && !c.boolFlag("stdout") && !c.interactive {
		_, err := c.out.Write(d.Data)
		return err
	}
	return c.save(d)
}

func toolShacl(c *Ctx) error {
	model, err := c.file("model", "Rdf model")
	if err != nil {
		return err
	}
	shapes, err := c.file("shapes", "Shacl shapes")
	if err != nil {
		return err
	}
	var out string
	if err := c.api.Multipart("/api/toolbox/public/rdf/shacl-validation", nil, []Part{FFile("modelFile", model), FFile("shaclFile", shapes)}, &out); err != nil {
		return err
	}
	if strings.TrimSpace(out) == "" {
		info("valid")
		return nil
	}
	fmt.Fprintln(c.out, out)
	return nil
}

func toolXpath(c *Ctx) error {
	expr, err := c.reqPos("expression", "XPath expression", "")
	if err != nil {
		return err
	}
	path, err := c.file("file", "Xml file")
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var out string
	if err := c.api.Multipart("/api/toolbox/public/xpath/evaluate", nil, []Part{F("expression", expr), F("data", string(data))}, &out); err != nil {
		return err
	}
	fmt.Fprintln(c.out, out)
	return nil
}
