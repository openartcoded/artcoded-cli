package main

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

var postStatuses = []string{"DRAFT", "IN_PROGRESS", "PENDING", "DONE", "CANCELLED"}
var priorities = []string{"LOW", "MEDIUM", "HIGH", "URGENT"}

func init() {
	register(&Module{
		Name: "reports", Aliases: []string{"report", "blog", "posts"}, Title: "Activity report", Routes: []string{"blog", "report"},
		Help: "activity reports: posts, attachments, messages, pdf",
		Actions: []*Action{
			{Name: "list", Aliases: []string{"ls", "search"}, Help: "search the reports", Usage: "[--title .. --status .. --priority .. --tag .. --bookmarked --page n]", Run: postList},
			{Name: "show", Help: "a report", Usage: "<id>", Run: postShow},
			{Name: "new", Aliases: []string{"add", "create"}, Help: "write a new report", Usage: "[--title .. --description .. --content-file f --tags a,b --status .. --priority .. --due date --cover img]", Run: postNew},
			{Name: "edit", Help: "edit a report (content in the editor)", Usage: "<id>", Run: postEdit},
			{Name: "attach", Help: "add files to a report", Usage: "<id> <file...>", Run: postAttach},
			{Name: "detach", Help: "remove a file of a report", Usage: "<id> [--attachment <upload id>]", Run: postDetach},
			{Name: "processed", Help: "mark a file of a report processed / not processed", Usage: "<id> [--attachment <upload id>]", Run: postProcessed},
			{Name: "download", Help: "files of a report", Usage: "<id> [--out dir]", Run: postDownload},
			{Name: "pdf", Help: "pdf of a report", Usage: "<id> [--out path --open]", Run: postPdf},
			{Name: "todos", Help: "edit the post-its (todo / in progress / done) of a report", Usage: "<id>", Run: postTodos},
			{Name: "messages", Aliases: []string{"channel"}, Help: "messages of a report", Usage: "<id>", Run: postMessages},
			{Name: "post", Aliases: []string{"message", "reply"}, Help: "post a message on a report", Usage: "<id> --message .. [--file f]...", Run: postMessage},
			{Name: "unread", Help: "unread messages per status", Run: postUnread},
			{Name: "tags", Help: "tags used by the reports", Run: func(c *Ctx) error {
				var l []string
				if err := c.api.Get("/api/report/tags", nil, &l); err != nil {
					return err
				}
				return c.result(l, func() { fmt.Fprintln(c.out, strings.Join(l, "\n")) })
			}},
			{Name: "bookmark", Help: "bookmark / unbookmark", Usage: "<id>", Run: func(c *Ctx) error {
				return toggleBookmark(c, "/api/report/toggle-bookmarked", func() ([]M, error) { return searchPosts(c, M{}) }, postDesc)
			}},
			{Name: "delete", Aliases: []string{"rm"}, Help: "delete a report", Usage: "<id>", Run: postDelete},
		},
	})
	register(&Module{
		Name: "gallery", Aliases: []string{"memzagram", "memz"}, Title: "Gallery", Routes: []string{"memzagram"},
		Help: "images of the public gallery",
		Actions: []*Action{
			{Name: "list", Aliases: []string{"ls"}, Help: "images", Usage: "[--page n]", Run: memzList},
			{Name: "add", Aliases: []string{"upload", "new"}, Help: "add an image", Usage: "<image> [--title .. --description .. --visible --date YYYY-MM-DD]", Run: memzAdd},
			{Name: "edit", Help: "change title, description, visibility or the image", Usage: "<id>", Run: memzEdit},
			{Name: "download", Help: "download an image", Usage: "<id>", Run: memzDownload},
			{Name: "delete", Aliases: []string{"rm"}, Help: "delete an image", Usage: "<id>", Run: memzDelete},
		},
	})
	register(&Module{
		Name: "cv", Aliases: []string{"curriculum"}, Title: "CV", Routes: []string{"cv"},
		Help: "curriculum: edit, download, download requests",
		Actions: []*Action{
			{Name: "show", Help: "the curriculum", Run: cvShow},
			{Name: "edit", Help: "edit the curriculum (json in the editor)", Run: cvEdit},
			{Name: "download", Aliases: []string{"pdf"}, Help: "pdf of the curriculum", Usage: "[--out path --open]", Run: func(c *Ctx) error {
				d, err := c.api.Fetch(http.MethodGet, "/api/cv/admin-download", nil, nil, "cv.pdf")
				if err != nil {
					return err
				}
				return c.save(d)
			}},
			{Name: "requests", Help: "download requests of the cv", Run: cvRequests},
			{Name: "request-delete", Help: "delete download requests", Usage: "<id...>", Run: cvRequestDelete},
			{Name: "templates", Help: "cv templates", Run: func(c *Ctx) error { c.args.flags["type"] = []string{"CV"}; return tplList(c) }},
		},
	})
}

// ---------- reports ----------

func searchPosts(c *Ctx, cr M) ([]M, error) {
	var p M
	if err := c.api.Post("/api/report/admin-search", Q("page", 0, "size", 100, "sort", "updatedDate,DESC"), cr, &p); err != nil {
		return nil, err
	}
	return content(p), nil
}

func postDesc(p M) string {
	return str(p, "title") + dim("  "+strings.ToLower(str(p, "status"))+"  "+fdate(p, "updatedDate"))
}

func postList(c *Ctx) error {
	cr := M{}
	for flag, field := range map[string]string{"title": "title", "content": "content", "tag": "tag"} {
		if v, ok := c.flag(flag); ok {
			cr[field] = v
		}
	}
	for flag, field := range map[string]string{"status": "status", "priority": "priority"} {
		if v, ok := c.flag(flag); ok {
			cr[field] = strings.ToUpper(v)
		}
	}
	if c.boolFlag("bookmarked") {
		cr["bookmarked"] = true
	}
	page, size, err := c.pageParams(20)
	if err != nil {
		return err
	}
	var p M
	if err := c.api.Post("/api/report/admin-search", Q("page", page, "size", size, "sort", "updatedDate,DESC"), cr, &p); err != nil {
		return err
	}
	return c.result(p, func() {
		rows := [][]string{}
		for _, x := range content(p) {
			unread := ""
			if n := num(x, "unreadCount"); n > 0 {
				unread = yellow(fmt.Sprintf("%d ✉", int(n)))
			}
			rows = append(rows, []string{str(x, "title"), strings.ToLower(str(x, "status")), strings.ToLower(str(x, "priority")), fdate(x, "dueDate"), str(x, "tags"), unread, mark(boolean(x, "bookmarked"), "★"), dim(str(x, "id"))})
		}
		table(c.out, []string{"title", "status", "priority", "due", "tags", "", "", "id"}, rows)
		fmt.Fprintln(c.out, dim(pageInfo(p)))
	})
}

func pickPost(c *Ctx) (M, error) {
	m, err := c.pick("report", func() ([]M, error) { return searchPosts(c, M{}) }, postDesc)
	if err != nil {
		return nil, err
	}
	var p M
	if err := c.api.Post("/api/report/post-by-id", Q("id", str(m, "id")), nil, &p); err != nil {
		return nil, err
	}
	return p, nil
}

func postShow(c *Ctx) error {
	p, err := pickPost(c)
	if err != nil {
		return err
	}
	return c.result(p, func() {
		details(c.out, "title", bold(str(p, "title")), "description", str(p, "description"), "status", str(p, "status"), "priority", str(p, "priority"),
			"due", fdate(p, "dueDate"), "author", str(p, "author"), "tags", str(p, "tags"), "updated", fdatetime(p, "updatedDate"), "id", str(p, "id"))
		if s := strings.TrimSpace(str(p, "content")); s != "" {
			fmt.Fprintln(c.out, "\n"+s)
		}
		for _, col := range []struct{ key, title string }{{"todos", "To do"}, {"inProgress", "In progress"}, {"done", "Done"}} {
			if items := maps(p[col.key]); len(items) > 0 {
				fmt.Fprintln(c.out, "\n"+bold(col.title))
				for _, it := range items {
					fmt.Fprintf(c.out, "  • %s\n", str(it, "note"))
				}
			}
		}
		processed := map[string]bool{}
		for _, id := range strList(p, "processedAttachmentIds") {
			processed[id] = true
		}
		if ups, _ := uploadsByIDs(c, strList(p, "attachmentIds")); len(ups) > 0 {
			fmt.Fprintln(c.out, "\n"+bold("Files"))
			for _, u := range ups {
				fmt.Fprintf(c.out, "  %s %s %s\n", mark(processed[str(u, "id")], green("✓")), str(u, "originalFilename"), dim(str(u, "id")))
			}
		}
	})
}

// postForm asks the fields of a report (defaults: the current values) and submits it
func postForm(c *Ctx, p M) (M, error) {
	var me M
	_ = c.api.Get("/api/personal-info/@me", nil, &me)
	title, err := c.req("title", "Title", str(p, "title"))
	if err != nil {
		return nil, err
	}
	desc, err := c.req("description", "Description (short)", str(p, "description"))
	if err != nil {
		return nil, err
	}
	content, err := c.longText("content", "Content (markdown)", str(p, "content"), false)
	if err != nil {
		return nil, err
	}
	tags, err := c.opt("tags", "Tags (comma separated)", strings.Join(strList(p, "tags"), ","))
	if err != nil {
		return nil, err
	}
	status, err := c.enum("status", "Status", postStatuses, defStr(str(p, "status"), "DRAFT"))
	if err != nil {
		return nil, err
	}
	prio, err := c.enum("priority", "Priority", priorities, defStr(str(p, "priority"), "MEDIUM"))
	if err != nil {
		return nil, err
	}
	dueDef := fdate(p, "dueDate")
	if dueDef == "" {
		dueDef = time.Now().In(brussels).AddDate(0, 1, 0).Format("2006-01-02")
	}
	due, err := c.date("due", "Due date", dueDef, true)
	if err != nil {
		return nil, err
	}
	author := c.quiet("author", defStr(str(p, "author"), str(me, "email")))
	parts := []Part{F("id", str(p, "id")), F("title", title), F("description", desc), F("content", content), F("tags", strings.Join(splitTags(tags), ",")),
		F("status", status), F("priority", prio), F("dueDate", httpDate(*due)), F("author", author)}
	if cover, ok := c.flag("cover"); ok {
		parts = append(parts, FFile("cover", expandPath(cover)))
	}
	var saved M
	if err := c.api.Multipart("/api/report/submit", nil, parts, &saved); err != nil {
		return nil, err
	}
	return saved, nil
}

func defStr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func postNew(c *Ctx) error {
	var p M
	if err := c.api.Post("/api/report/new-post", nil, nil, &p); err != nil {
		return err
	}
	saved, err := postForm(c, p)
	if err != nil {
		_ = c.api.Delete("/api/report", Q("id", str(p, "id")), nil)
		return err
	}
	return c.result(saved, func() { info("report %q saved (%s)", str(saved, "title"), str(saved, "id")) })
}

func postEdit(c *Ctx) error {
	p, err := pickPost(c)
	if err != nil {
		return err
	}
	saved, err := postForm(c, p)
	if err != nil {
		return err
	}
	return c.result(saved, func() { info("report saved") })
}

func postAttach(c *Ctx) error {
	p, err := pickPost(c)
	if err != nil {
		return err
	}
	files, err := c.files("file", "File", true)
	if err != nil {
		return err
	}
	parts := []Part{F("id", str(p, "id"))}
	for _, f := range files {
		parts = append(parts, FFile("files", f))
	}
	if err := c.api.Multipart("/api/report/add-attachment", nil, parts, nil); err != nil {
		return err
	}
	info("%d file(s) added", len(files))
	return nil
}

func postDetach(c *Ctx) error {
	p, err := pickPost(c)
	if err != nil {
		return err
	}
	id, err := chooseUpload(c, strList(p, "attachmentIds"))
	if err != nil {
		return err
	}
	if err := c.confirmAction("Remove this file?"); err != nil {
		return err
	}
	if err := c.api.Post("/api/report/remove-attachment", Q("id", str(p, "id"), "attachmentId", id), nil, nil); err != nil {
		return err
	}
	info("removed")
	return nil
}

func postProcessed(c *Ctx) error {
	p, err := pickPost(c)
	if err != nil {
		return err
	}
	id, err := chooseUpload(c, strList(p, "attachmentIds"))
	if err != nil {
		return err
	}
	var saved M
	if err := c.api.Post("/api/report/toggle-process-attachment", Q("id", str(p, "id"), "attachmentId", id), nil, &saved); err != nil {
		return err
	}
	done := false
	for _, x := range strList(saved, "processedAttachmentIds") {
		done = done || x == id
	}
	info("processed: %s", yesNo(done))
	return nil
}

func postDownload(c *Ctx) error {
	p, err := pickPost(c)
	if err != nil {
		return err
	}
	return downloadAll(c, strList(p, "attachmentIds"))
}

func postPdf(c *Ctx) error {
	p, err := pickPost(c)
	if err != nil {
		return err
	}
	d, err := c.api.Fetch(http.MethodGet, "/api/report/generate-pdf", Q("id", str(p, "id")), nil, str(p, "title")+".pdf")
	if err != nil {
		return err
	}
	return c.save(d)
}

func postTodos(c *Ctx) error {
	p, err := pickPost(c)
	if err != nil {
		return err
	}
	cur := M{"todos": orEmpty(p["todos"]), "inProgress": orEmpty(p["inProgress"]), "done": orEmpty(p["done"])}
	var edited M
	fmt.Fprintln(c.out, dim(`move {"note": "..."} items between todos / inProgress / done`))
	changed, err := editJSON(c.cfg, cur, &edited)
	if err != nil || !changed {
		if err == nil {
			info("no change")
		}
		return err
	}
	if err := c.api.Post("/api/report/update-post-it", Q("id", str(p, "id")), edited, nil); err != nil {
		return err
	}
	info("post-its saved")
	return nil
}

func orEmpty(v any) any {
	if v == nil {
		return []any{}
	}
	return v
}

func postMessages(c *Ctx) error {
	p, err := pickPost(c)
	if err != nil {
		return err
	}
	var ch M
	if err := c.api.Post("/api/report/channel/get", Q("id", str(p, "id")), nil, &ch); err != nil {
		return err
	}
	_ = c.api.Post("/api/report/channel/read", Q("id", str(p, "id")), nil, nil)
	return c.result(ch, func() {
		msgs := maps(ch["messages"])
		if len(msgs) == 0 {
			fmt.Fprintln(c.out, dim("(no message)"))
		}
		for _, m := range msgs {
			fmt.Fprintf(c.out, "%s %s %s\n%s\n", bold(str(m, "emailFrom")), dim(fdatetime(m, "creationDate")), dim(str(m, "id")), str(m, "content"))
			if ids := strList(m, "attachmentIds"); len(ids) > 0 {
				fmt.Fprintf(c.out, dim("  %d file(s): %s\n"), len(ids), strings.Join(ids, ", "))
			}
			fmt.Fprintln(c.out)
		}
	})
}

func postMessage(c *Ctx) error {
	p, err := pickPost(c)
	if err != nil {
		return err
	}
	msg, err := c.longText("message", "Message", "", true)
	if err != nil {
		return err
	}
	parts := []Part{F("id", str(p, "id")), F("message", msg)}
	for _, f := range c.flags("file") {
		parts = append(parts, FFile("files", expandPath(f)))
	}
	if err := c.api.Multipart("/api/report/channel/post", nil, parts, nil); err != nil {
		return err
	}
	info("message posted")
	return nil
}

func postUnread(c *Ctx) error {
	var l []M
	if err := c.api.Post("/api/report/channel/unread-count", nil, nil, &l); err != nil {
		return err
	}
	return c.result(l, func() {
		rows := [][]string{}
		for _, u := range l {
			rows = append(rows, []string{str(u, "status"), str(u, "subscriber"), str(u, "counter")})
		}
		table(c.out, []string{"status", "subscriber", "unread"}, rows)
	})
}

func postDelete(c *Ctx) error {
	p, err := pickPost(c)
	if err != nil {
		return err
	}
	if err := c.confirmAction("Delete " + str(p, "title") + "?"); err != nil {
		return err
	}
	if err := c.api.Delete("/api/report", Q("id", str(p, "id")), nil); err != nil {
		return err
	}
	info("deleted")
	return nil
}

// ---------- gallery ----------

func loadMemz(c *Ctx, page, size int) (M, error) {
	var p M
	return p, c.api.Get("/api/memzagram/all", Q("page", page, "size", size, "sort", "updatedDate,DESC"), &p)
}

func memzDesc(m M) string {
	return str(m, "title") + dim("  "+fdate(m, "dateOfVisibility")+mark(!boolean(m, "visible"), " hidden"))
}

func memzList(c *Ctx) error {
	page, size, err := c.pageParams(30)
	if err != nil {
		return err
	}
	p, err := loadMemz(c, page, size)
	if err != nil {
		return err
	}
	return c.result(p, func() {
		rows := [][]string{}
		for _, m := range content(p) {
			rows = append(rows, []string{str(m, "title"), str(m, "description"), yesNo(boolean(m, "visible")), fdate(m, "dateOfVisibility"), str(m, "viewsCount"), dim(str(m, "id"))})
		}
		table(c.out, []string{"title", "description", "visible", "visible from", "views", "id"}, rows)
		fmt.Fprintln(c.out, dim(pageInfo(p)))
	})
}

func pickMemz(c *Ctx) (M, error) {
	all := func() ([]M, error) {
		p, err := loadMemz(c, 0, 200)
		return content(p), err
	}
	m, err := c.pick("image", all, memzDesc)
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
	return nil, fmt.Errorf("image %s not found", str(m, "id"))
}

func memzSubmit(c *Ctx, m M, image string) error {
	title, err := c.req("title", "Title", defStr(str(m, "title"), strings.TrimSuffix(filepath.Base(image), filepath.Ext(image))))
	if err != nil {
		return err
	}
	desc, err := c.opt("description", "Description", str(m, "description"))
	if err != nil {
		return err
	}
	visible, err := c.boolOpt("visible", "Visible in the public gallery?", m == nil || boolean(m, "visible"))
	if err != nil {
		return err
	}
	parts := []Part{F("title", title), F("description", desc), F("visible", fmt.Sprint(visible))}
	if id := str(m, "id"); id != "" {
		parts = append(parts, F("id", id))
	}
	if d, err := c.date("date", "Visible from", fdate(m, "dateOfVisibility"), false); err != nil {
		return err
	} else if d != nil {
		parts = append(parts, F("dateOfVisibility", httpDate(*d)))
	}
	if image != "" {
		parts = append(parts, FFile("imageUpload", image))
	}
	if err := c.api.Multipart("/api/memzagram/submit", nil, parts, nil); err != nil {
		return err
	}
	info("saved (thumbnails are made in the background)")
	return nil
}

func memzAdd(c *Ctx) error {
	img, err := c.file("file", "Image")
	if err != nil {
		return err
	}
	return memzSubmit(c, nil, img)
}

func memzEdit(c *Ctx) error {
	m, err := pickMemz(c)
	if err != nil {
		return err
	}
	img := c.quiet("file", "")
	if img == "" && c.interactive && !c.yes {
		if img, err = ask("Replace the image (path, empty to keep)", ""); err != nil {
			return err
		}
	}
	if img != "" {
		img = expandPath(img)
	}
	return memzSubmit(c, m, img)
}

func memzDownload(c *Ctx) error {
	m, err := pickMemz(c)
	if err != nil {
		return err
	}
	return downloadAll(c, []string{str(m, "imageUploadId")})
}

func memzDelete(c *Ctx) error {
	m, err := pickMemz(c)
	if err != nil {
		return err
	}
	if err := c.confirmAction("Delete " + str(m, "title") + "?"); err != nil {
		return err
	}
	if err := c.api.Delete("/api/memzagram", Q("id", str(m, "id")), nil); err != nil {
		return err
	}
	info("deleted")
	return nil
}

// ---------- cv ----------

func cvShow(c *Ctx) error {
	var cv M
	if err := c.api.Post("/api/cv/full", nil, nil, &cv); err != nil {
		return err
	}
	return c.result(cv, func() {
		details(c.out, "name", bold(str(cv, "person.firstname")+" "+str(cv, "person.lastname")), "title", str(cv, "person.title"),
			"email", str(cv, "person.emailAddress"), "phone", str(cv, "person.phoneNumber"), "website", str(cv, "person.website"),
			"linkedin", str(cv, "person.linkedinUrl"), "github", str(cv, "person.githubUrl"), "updated", fdatetime(cv, "updatedDate"))
		if s := str(cv, "introduction"); s != "" {
			fmt.Fprintln(c.out, "\n"+s)
		}
		fmt.Fprintln(c.out, "\n"+bold("Experiences"))
		for _, e := range maps(cv["experiences"]) {
			to := fdate(e, "to")
			if boolean(e, "current") {
				to = "now"
			}
			fmt.Fprintf(c.out, "  %s → %s  %s @ %s\n", fdate(e, "from"), to, bold(str(e, "title")), str(e, "company"))
		}
		var skills []string
		for _, s := range maps(cv["skills"]) {
			skills = append(skills, str(s, "name"))
		}
		if len(skills) > 0 {
			fmt.Fprintln(c.out, "\n"+bold("Skills")+"\n  "+strings.Join(skills, ", "))
		}
		fmt.Fprintf(c.out, "\n%s %d schools, %d projects, %d hobbies\n", dim("also:"), len(list(cv, "scholarHistories")), len(list(cv, "personalProjects")), len(list(cv, "hobbies")))
	})
}

func cvEdit(c *Ctx) error {
	var cv M
	if err := c.api.Post("/api/cv/full", nil, nil, &cv); err != nil {
		return err
	}
	var edited M
	changed, err := editJSON(c.cfg, cv, &edited)
	if err != nil || !changed {
		if err == nil {
			info("no change")
		}
		return err
	}
	if err := c.api.Post("/api/cv/update", nil, edited, nil); err != nil {
		return err
	}
	info("cv saved")
	return nil
}

func loadCvRequests(c *Ctx) ([]M, error) {
	var l []M
	return l, c.api.Post("/api/cv/download-requests", nil, nil, &l)
}

func cvRequests(c *Ctx) error {
	l, err := loadCvRequests(c)
	if err != nil {
		return err
	}
	return c.result(l, func() {
		rows := [][]string{}
		for _, r := range l {
			rows = append(rows, []string{fdatetime(r, "dateReceived"), str(r, "email"), str(r, "phoneNumber"), mark(boolean(r, "dailyRate"), "rate") + " " + mark(boolean(r, "availability"), "availability"), dim(str(r, "id"))})
		}
		table(c.out, []string{"received", "email", "phone", "asks", "id"}, rows)
	})
}

func cvRequestDelete(c *Ctx) error {
	ids, err := c.pickMany("requests", func() ([]M, error) { return loadCvRequests(c) }, func(r M) string { return fdate(r, "dateReceived") + "  " + str(r, "email") })
	if err != nil {
		return err
	}
	if err := c.confirmAction(fmt.Sprintf("Delete %d request(s)?", len(ids))); err != nil {
		return err
	}
	for _, id := range ids {
		if err := c.api.Delete("/api/cv/download-requests", Q("id", id), nil); err != nil {
			return err
		}
	}
	info("deleted")
	return nil
}
