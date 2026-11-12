package main

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func init() {
	register(&Module{
		Name: "dossiers", Aliases: []string{"dossier"}, Title: "Dossiers", Routes: []string{"dossier"},
		Help: "accounting dossiers: expenses, invoices, documents, close, reports",
		Actions: []*Action{
			{Name: "active", Aliases: []string{"current"}, Help: "the active dossier and what it contains", Run: dosActive},
			{Name: "list", Aliases: []string{"ls"}, Help: "dossiers", Usage: "[--closed --bookmarked --page n]", Run: dosList},
			{Name: "show", Help: "a dossier", Usage: "<id>", Run: dosShow},
			{Name: "new", Aliases: []string{"create"}, Help: "open a new dossier", Usage: "[--name .. --description .. --from-previous]", Run: dosNew},
			{Name: "edit", Help: "edit the active dossier (json in the editor)", Run: dosEdit},
			{Name: "add-expenses", Aliases: []string{"add-fees"}, Help: "add expenses to the active dossier", Usage: "[<expense id...>] [--tag T]", Run: dosAddFees},
			{Name: "remove-expense", Help: "remove an expense from the active dossier", Usage: "<expense id>", Run: dosRemoveFee},
			{Name: "add-invoice", Help: "add an invoice to the active dossier", Usage: "<invoice id>", Run: dosAddInvoice},
			{Name: "remove-invoice", Help: "remove an invoice from the active dossier", Usage: "<invoice id>", Run: dosRemoveInvoice},
			{Name: "add-document", Help: "add a document to the active dossier", Usage: "<document id>", Run: dosAddDocument},
			{Name: "remove-document", Help: "remove a document from the active dossier", Usage: "<document id>", Run: dosRemoveDocument},
			{Name: "close", Help: "close the active dossier (zip sent to the accountant)", Run: dosClose},
			{Name: "recall", Help: "reopen a closed dossier for modification", Usage: "<id>", Run: dosRecall},
			{Name: "delete-active", Help: "delete the active dossier", Run: dosDeleteActive},
			{Name: "download", Aliases: []string{"zip"}, Help: "zip of a closed dossier", Usage: "<id> [--out path]", Run: dosDownload},
			{Name: "summary-pdf", Help: "summary pdf of a dossier", Usage: "<id>", Run: dosSummaryPdf},
			{Name: "report", Help: "pdf report of a dossier compared with the previous years", Usage: "<id> [--month 2026-10]", Run: dosReport},
			{Name: "annual-report", Help: "pdf annual report of a year", Usage: "<year>", Run: dosAnnualReport},
			{Name: "summaries", Help: "totals of the dossiers", Usage: "[--closed]", Run: dosSummaries},
			{Name: "by-year", Help: "closed dossiers by year", Run: dosByYear},
			{Name: "size", Help: "size of the files of a dossier", Usage: "<id>", Run: dosSize},
			{Name: "bookmark", Help: "bookmark / unbookmark", Usage: "<id>", Run: func(c *Ctx) error {
				return toggleBookmark(c, "/api/dossier/toggle-bookmarked", func() ([]M, error) { return loadDossiers(c, true) }, dosDesc)
			}},
			{Name: "import", Help: "import a dossier from a zip", Usage: "<zip>", Run: dosImport},
			{Name: "import-example", Help: "download the xlsx example of an import", Run: func(c *Ctx) error {
				d, err := c.api.Fetch(http.MethodGet, "/api/dossier/import-example", nil, nil, "import-example.xlsx")
				if err != nil {
					return err
				}
				return c.save(d)
			}},
		},
	})
	register(&Module{
		Name: "documents", Aliases: []string{"document", "docs"}, Title: "Documents", Routes: []string{"documents"},
		Help: "administrative documents: upload, tag, download",
		Actions: []*Action{
			{Name: "list", Aliases: []string{"ls", "search"}, Help: "search the documents", Usage: "[--title .. --tag .. --from date --to date --bookmarked --page n]", Run: docList},
			{Name: "show", Help: "a document", Usage: "<id>", Run: docShow},
			{Name: "upload", Aliases: []string{"add", "new"}, Help: "upload a document", Usage: "<file> [--title .. --description .. --tags a,b]", Run: docUpload},
			{Name: "edit", Help: "change title, description, tags or the file", Usage: "<id> [--title .. --description .. --tags a,b --file path]", Run: docEdit},
			{Name: "download", Help: "download a document", Usage: "<id> [--out path --open]", Run: docDownload},
			{Name: "bookmark", Help: "bookmark / unbookmark", Usage: "<id>", Run: func(c *Ctx) error {
				return toggleBookmark(c, "/api/administrative-document/toggle-bookmarked", func() ([]M, error) { return searchDocs(c, M{"bookmarked": false}) }, docDesc)
			}},
			{Name: "delete", Aliases: []string{"rm"}, Help: "delete a document", Usage: "<id>", Run: docDelete},
		},
	})
}

func loadDossiers(c *Ctx, closed bool) ([]M, error) {
	var l []M
	return l, c.api.Post("/api/dossier/find-all", Q("closed", closed), nil, &l)
}

func dosDesc(d M) string {
	return str(d, "name") + dim("  "+fdate(d, "closedDate"))
}

func activeDossier(c *Ctx) (M, error) {
	var d M
	if err := c.api.Post("/api/dossier/active-dossier", nil, nil, &d); err != nil {
		return nil, err
	}
	if d == nil || str(d, "id") == "" {
		return nil, errors.New("no active dossier (dossiers new)")
	}
	return d, nil
}

func printDossier(c *Ctx, d M) {
	var sum M
	_ = c.api.Post("/api/dossier/summary", Q("id", str(d, "id")), nil, &sum)
	state := yellow("active")
	if boolean(d, "closed") {
		state = dim("closed " + fdate(d, "closedDate"))
	}
	if boolean(d, "recalledForModification") {
		state += yellow(" (recalled)")
	}
	details(c.out, "dossier", bold(str(d, "name")), "status", state, "description", str(d, "description"),
		"expenses", fmt.Sprint(len(list(d, "feeIds"))), "invoices", fmt.Sprint(len(list(d, "invoiceIds"))), "documents", fmt.Sprint(len(list(d, "documentIds"))),
		"vat due", moneyOf(d, "tvaDue"), "vat to pay", moneyOf(d, "tvaToBePaid"),
		"total expenses", moneyOf(sum, "totalExpenses"), "total earnings", moneyOf(sum, "totalEarnings"), "comment", str(d, "comment"), "id", str(d, "id"))
	for _, a := range maps(d["advancePayments"]) {
		fmt.Fprintf(c.out, "  vat advance %s on %s\n", moneyOf(a, "advance"), fdate(a, "datePaid"))
	}
}

func dosActive(c *Ctx) error {
	d, err := activeDossier(c)
	if err != nil {
		return err
	}
	return c.result(d, func() {
		printDossier(c, d)
		if ids := strList(d, "feeIds"); len(ids) > 0 {
			var fees []M
			if c.api.Post("/api/fee/find-by-ids", Q("id", ids), nil, &fees) == nil {
				fmt.Fprintln(c.out, "\n"+bold("Expenses"))
				rows := [][]string{}
				for _, f := range fees {
					rows = append(rows, []string{fdate(f, "date"), str(f, "subject"), str(f, "tag"), moneyOf(f, "priceTot"), dim(str(f, "id"))})
				}
				table(c.out, []string{"date", "subject", "tag", "total", "id"}, rows)
			}
		}
		if ids := strList(d, "invoiceIds"); len(ids) > 0 {
			var invs []M
			if c.api.Post("/api/invoice/find-by-ids", Q("id", ids), nil, &invs) == nil {
				fmt.Fprintln(c.out, "\n"+bold("Invoices"))
				rows := [][]string{}
				for _, i := range invs {
					rows = append(rows, []string{invNumber(i), fdate(i, "dateOfInvoice"), str(i, "billTo.clientName"), moneyOf(i, "total"), dim(str(i, "id"))})
				}
				table(c.out, []string{"number", "date", "client", "total", "id"}, rows)
			}
		}
		if ids := strList(d, "documentIds"); len(ids) > 0 {
			var docs []M
			if c.api.Post("/api/administrative-document/find-by-ids", Q("id", ids), nil, &docs) == nil {
				fmt.Fprintln(c.out, "\n"+bold("Documents"))
				for _, x := range docs {
					fmt.Fprintf(c.out, "  %s %s\n", str(x, "title"), dim(str(x, "id")))
				}
			}
		}
	})
}

func dosList(c *Ctx) error {
	page, size, err := c.pageParams(20)
	if err != nil {
		return err
	}
	var p M
	q := Q("page", page, "size", size, "sort", "creationDate,DESC")
	if c.boolFlag("bookmarked") {
		err = c.api.Post("/api/dossier/bookmarked", q, nil, &p)
	} else {
		q.Set("closed", fmt.Sprint(c.boolFlag("closed")))
		err = c.api.Post("/api/dossier/find-all-paged", q, nil, &p)
	}
	if err != nil {
		return err
	}
	return c.result(p, func() {
		rows := [][]string{}
		for _, d := range content(p) {
			rows = append(rows, []string{str(d, "name"), fdate(d, "creationDate"), fdate(d, "closedDate"), fmt.Sprint(len(list(d, "feeIds"))), fmt.Sprint(len(list(d, "invoiceIds"))),
				moneyOf(d, "tvaDue"), mark(boolean(d, "bookmarked"), "★"), dim(str(d, "id"))})
		}
		if c.boolFlag("closed") {
			fmt.Fprintln(c.out, bold("Closed dossiers"))
		} else if !c.boolFlag("bookmarked") {
			fmt.Fprintln(c.out, bold("Open dossiers")+dim(" · the closed ones: --closed"))
		}
		table(c.out, []string{"dossier", "created", "closed", "expenses", "invoices", "vat due", "", "id"}, rows)
		fmt.Fprintln(c.out, dim(pageInfo(p)))
	})
}

func pickDossier(c *Ctx) (M, error) {
	m, err := c.pick("dossier", func() ([]M, error) {
		a, err := loadDossiers(c, false)
		if err != nil {
			return nil, err
		}
		b, _ := loadDossiers(c, true)
		return append(a, b...), nil
	}, dosDesc)
	if err != nil {
		return nil, err
	}
	var d M
	if err := c.api.Post("/api/dossier/find-by-id", Q("id", str(m, "id")), nil, &d); err != nil {
		return nil, err
	}
	return d, nil
}

func dosShow(c *Ctx) error {
	d, err := pickDossier(c)
	if err != nil {
		return err
	}
	return c.result(d, func() { printDossier(c, d) })
}

func dosNew(c *Ctx) error {
	var d M
	if c.boolFlag("from-previous") {
		if err := c.api.Post("/api/dossier/new-from-previous", nil, nil, &d); err != nil {
			return err
		}
		return c.result(d, func() { info("dossier %s created", str(d, "name")) })
	}
	def := time.Now().In(brussels).Format("2006-01")
	name, err := c.reqPos("name", "Name", def)
	if err != nil {
		return err
	}
	desc, err := c.opt("description", "Description", "")
	if err != nil {
		return err
	}
	body := M{"name": name, "description": desc}
	if v, err := c.num("vat-due", "VAT due (optional)", nil, false); err != nil {
		return err
	} else if v != nil {
		body["tvaDue"] = *v
	}
	if err := c.api.Post("/api/dossier/new-dossier", nil, body, &d); err != nil {
		return err
	}
	return c.result(d, func() { info("dossier %s created", str(d, "name")) })
}

func dosEdit(c *Ctx) error {
	d, err := activeDossier(c)
	if err != nil {
		return err
	}
	var edited M
	changed, err := editJSON(c.cfg, d, &edited)
	if err != nil || !changed {
		if err == nil {
			info("no change")
		}
		return err
	}
	if err := c.api.Post("/api/dossier/update-dossier", nil, edited, nil); err != nil {
		return err
	}
	info("dossier saved")
	return nil
}

func dosAddFees(c *Ctx) error {
	cr := M{"archived": false}
	if t, ok := c.flag("tag"); ok {
		cr["tag"] = t
	}
	ids, err := c.pickMany("expenses to add", func() ([]M, error) {
		l, err := searchFees(c, cr, 0, 200)
		if err != nil {
			return nil, err
		}
		var tagged []M
		for _, f := range l {
			if str(f, "tag") != "" {
				tagged = append(tagged, f)
			}
		}
		if len(tagged) < len(l) {
			warn("%d expense(s) without tag not shown (tag them first)", len(l)-len(tagged))
		}
		return tagged, nil
	}, feeDesc)
	if err != nil {
		return err
	}
	if err := c.api.Post("/api/dossier/process-fees", nil, ids, nil); err != nil {
		return err
	}
	info("%d expense(s) added to the active dossier", len(ids))
	return nil
}

func dosRemoveFee(c *Ctx) error {
	d, err := activeDossier(c)
	if err != nil {
		return err
	}
	id, err := c.pickID("expense", func() ([]M, error) {
		var fees []M
		return fees, c.api.Post("/api/fee/find-by-ids", Q("id", strList(d, "feeIds")), nil, &fees)
	}, feeDesc)
	if err != nil {
		return err
	}
	if err := c.api.Post("/api/dossier/remove-fee", Q("feeId", id), nil, nil); err != nil {
		return err
	}
	info("removed")
	return nil
}

func dosAddInvoice(c *Ctx) error {
	id, err := c.pickID("invoice", func() ([]M, error) {
		l, err := searchInvoices(c, false, false, 0, 100)
		if err != nil {
			return nil, err
		}
		var gen []M
		for _, i := range l {
			if boolean(i, "locked") {
				gen = append(gen, i)
			}
		}
		return gen, nil
	}, invDesc)
	if err != nil {
		return err
	}
	if err := c.api.Post("/api/dossier/process-invoice", Q("id", id), nil, nil); err != nil {
		return err
	}
	info("invoice added to the active dossier")
	return nil
}

func dosRemoveInvoice(c *Ctx) error {
	d, err := activeDossier(c)
	if err != nil {
		return err
	}
	id, err := c.pickID("invoice", func() ([]M, error) {
		var l []M
		return l, c.api.Post("/api/invoice/find-by-ids", Q("id", strList(d, "invoiceIds")), nil, &l)
	}, invDesc)
	if err != nil {
		return err
	}
	if err := c.api.Post("/api/dossier/remove-invoice", Q("id", id), nil, nil); err != nil {
		return err
	}
	info("removed")
	return nil
}

func dosAddDocument(c *Ctx) error {
	id, err := c.pickID("document", func() ([]M, error) { return searchDocs(c, M{"bookmarked": false}) }, docDesc)
	if err != nil {
		return err
	}
	if err := c.api.Post("/api/dossier/add-document", Q("id", id), nil, nil); err != nil {
		return err
	}
	info("document added to the active dossier")
	return nil
}

func dosRemoveDocument(c *Ctx) error {
	d, err := activeDossier(c)
	if err != nil {
		return err
	}
	id, err := c.pickID("document", func() ([]M, error) {
		var l []M
		return l, c.api.Post("/api/administrative-document/find-by-ids", Q("id", strList(d, "documentIds")), nil, &l)
	}, docDesc)
	if err != nil {
		return err
	}
	if err := c.api.Post("/api/dossier/remove-document", Q("id", id), nil, nil); err != nil {
		return err
	}
	info("removed")
	return nil
}

func dosClose(c *Ctx) error {
	d, err := activeDossier(c)
	if err != nil {
		return err
	}
	if err := c.confirmAction("Close the dossier " + str(d, "name") + "? It is zipped and sent to the accountant"); err != nil {
		return err
	}
	if err := c.api.Post("/api/dossier/close-active-dossier", nil, nil, nil); err != nil {
		return err
	}
	info("dossier %s closed", str(d, "name"))
	return nil
}

func dosRecall(c *Ctx) error {
	d, err := c.pick("closed dossier", func() ([]M, error) { return loadDossiers(c, true) }, dosDesc)
	if err != nil {
		return err
	}
	if len(d) == 1 {
		if err := c.api.Post("/api/dossier/find-by-id", Q("id", str(d, "id")), nil, &d); err != nil {
			return err
		}
	}
	if err := c.confirmAction("Recall " + str(d, "name") + " for modification?"); err != nil {
		return err
	}
	if err := c.api.Post("/api/dossier/recall-for-modification", nil, d, nil); err != nil {
		return err
	}
	info("recalled")
	return nil
}

func dosDeleteActive(c *Ctx) error {
	d, err := activeDossier(c)
	if err != nil {
		return err
	}
	if err := c.confirmAction("Delete the active dossier " + str(d, "name") + "?"); err != nil {
		return err
	}
	if err := c.api.Delete("/api/dossier/active-dossier", nil, nil); err != nil {
		return err
	}
	info("deleted")
	return nil
}

func dosDownload(c *Ctx) error {
	d, err := pickDossier(c)
	if err != nil {
		return err
	}
	if str(d, "dossierUploadId") == "" {
		return errors.New("no zip: only closed dossiers have one")
	}
	dl, err := c.api.FileDownload(str(d, "dossierUploadId"))
	if err != nil {
		return err
	}
	return c.save(dl)
}

func dosSummaryPdf(c *Ctx) error {
	d, err := pickDossier(c)
	if err != nil {
		return err
	}
	dl, err := c.api.Fetch(http.MethodGet, "/api/dossier/generate-summary", Q("id", str(d, "id")), nil, "summary-"+str(d, "name")+".pdf")
	if err != nil {
		return err
	}
	return c.save(dl)
}

func dosReport(c *Ctx) error {
	d, err := pickDossier(c)
	if err != nil {
		return err
	}
	month := c.quiet("month", "")
	dl, err := c.api.Fetch(http.MethodGet, "/api/dossier/generate-report", Q("id", str(d, "id"), "month", month), nil, "report.pdf")
	if err != nil {
		return err
	}
	return c.save(dl)
}

func dosAnnualReport(c *Ctx) error {
	y, err := c.reqPos("year", "Year", strconv.Itoa(time.Now().Year()))
	if err != nil {
		return err
	}
	dl, err := c.api.Fetch(http.MethodGet, "/api/dossier/generate-annual-report", Q("year", y), nil, "annual-report-"+y+".pdf")
	if err != nil {
		return err
	}
	return c.save(dl)
}

func dosSummaries(c *Ctx) error {
	var l []M
	if err := c.api.Post("/api/dossier/find-all-summaries", Q("closed", c.boolFlag("closed")), nil, &l); err != nil {
		return err
	}
	return c.result(l, func() {
		rows := [][]string{}
		for _, s := range l {
			rows = append(rows, []string{str(s, "name"), fdate(s, "closedDate"), moneyOf(s, "totalExpenses"), moneyOf(s, "totalEarnings")})
		}
		table(c.out, []string{"dossier", "closed", "expenses", "earnings"}, rows)
	})
}

func dosByYear(c *Ctx) error {
	var l []M
	if err := c.api.Post("/api/dossier/closed-by-year", nil, nil, &l); err != nil {
		return err
	}
	return c.result(l, func() {
		for _, y := range l {
			fmt.Fprintln(c.out, bold(str(y, "year")))
			for _, d := range maps(y["dossiers"]) {
				fmt.Fprintf(c.out, "  %s %s\n", str(d, "name"), dim(fdate(d, "closedDate")+"  "+str(d, "id")))
			}
		}
	})
}

func dosSize(c *Ctx) error {
	d, err := pickDossier(c)
	if err != nil {
		return err
	}
	var size float64
	if err := c.api.Post("/api/dossier/size", Q("id", str(d, "id")), nil, &size); err != nil {
		return err
	}
	return c.result(size, func() { fmt.Fprintln(c.out, humanSize(size)) })
}

func dosImport(c *Ctx) error {
	path, err := c.file("file", "Zip of the dossier")
	if err != nil {
		return err
	}
	if err := c.api.Multipart("/api/dossier/import", nil, []Part{FFile("zip", path)}, nil); err != nil {
		return err
	}
	info("import started (%s)", filepath.Base(path))
	return nil
}

// ---------- documents ----------

func searchDocs(c *Ctx, cr M) ([]M, error) {
	var p M
	if err := c.api.Post("/api/administrative-document/search", Q("page", 0, "size", 200, "sort", "dateCreation,DESC"), cr, &p); err != nil {
		return nil, err
	}
	return content(p), nil
}

func docDesc(d M) string {
	s := str(d, "title") + dim("  "+fdate(d, "dateCreation"))
	if t := strList(d, "tags"); len(t) > 0 {
		s += "  " + cyan(strings.Join(t, ","))
	}
	return s
}

func docList(c *Ctx) error {
	cr := M{"bookmarked": c.boolFlag("bookmarked")}
	if v, ok := c.flag("title"); ok {
		cr["title"] = v
	}
	if v, ok := c.flag("description"); ok {
		cr["description"] = v
	}
	if v := c.flags("tag"); len(v) > 0 {
		cr["tags"] = v
	}
	for flag, field := range map[string]string{"from": "dateAfter", "to": "dateBefore"} {
		if v, ok := c.flag(flag); ok {
			t, err := parseDate(v)
			if err != nil {
				return err
			}
			cr[field] = iso(t)
		}
	}
	page, size, err := c.pageParams(30)
	if err != nil {
		return err
	}
	var p M
	if err := c.api.Post("/api/administrative-document/search", Q("page", page, "size", size, "sort", "dateCreation,DESC"), cr, &p); err != nil {
		return err
	}
	return c.result(p, func() {
		rows := [][]string{}
		for _, d := range content(p) {
			rows = append(rows, []string{str(d, "title"), fdate(d, "dateCreation"), str(d, "tags"), str(d, "description"), mark(boolean(d, "bookmarked"), "★") + mark(boolean(d, "locked"), "🔒"), dim(str(d, "id"))})
		}
		table(c.out, []string{"title", "date", "tags", "description", "", "id"}, rows)
		fmt.Fprintln(c.out, dim(pageInfo(p)))
	})
}

func pickDoc(c *Ctx) (M, error) {
	m, err := c.pick("document", func() ([]M, error) { return searchDocs(c, M{"bookmarked": false}) }, docDesc)
	if err != nil {
		return nil, err
	}
	if len(m) > 1 {
		return m, nil
	}
	var d M
	if err := c.api.Post("/api/administrative-document/find-by-id", Q("id", str(m, "id")), nil, &d); err != nil {
		return nil, err
	}
	return d, nil
}

func docShow(c *Ctx) error {
	d, err := pickDoc(c)
	if err != nil {
		return err
	}
	return c.result(d, func() {
		details(c.out, "title", bold(str(d, "title")), "description", str(d, "description"), "tags", str(d, "tags"),
			"created", fdatetime(d, "dateCreation"), "updated", fdatetime(d, "updatedDate"), "locked", mark(boolean(d, "locked"), "yes (in a closed dossier)"), "id", str(d, "id"))
		if a := str(d, "attachmentId"); a != "" {
			printUploads(c, "File", []string{a})
		}
	})
}

func splitTags(v string) []string {
	var out []string
	for _, t := range strings.Split(v, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func docUpload(c *Ctx) error {
	path, err := c.file("file", "File")
	if err != nil {
		return err
	}
	title, err := c.req("title", "Title", strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	if err != nil {
		return err
	}
	desc, err := c.opt("description", "Description", "")
	if err != nil {
		return err
	}
	tags, err := c.opt("tags", "Tags (comma separated)", "")
	if err != nil {
		return err
	}
	parts := []Part{F("title", title), F("description", desc), F("tags", strings.Join(splitTags(tags), ",")), FFile("document", path)}
	if err := c.api.Multipart("/api/administrative-document/save", nil, parts, nil); err != nil {
		return err
	}
	info("document %q uploaded", title)
	return nil
}

func docEdit(c *Ctx) error {
	d, err := pickDoc(c)
	if err != nil {
		return err
	}
	title, err := c.req("title", "Title", str(d, "title"))
	if err != nil {
		return err
	}
	desc, err := c.opt("description", "Description", str(d, "description"))
	if err != nil {
		return err
	}
	tags, err := c.opt("tags", "Tags (comma separated)", strings.Join(strList(d, "tags"), ","))
	if err != nil {
		return err
	}
	parts := []Part{F("id", str(d, "id")), F("title", title), F("description", desc), F("tags", strings.Join(splitTags(tags), ","))}
	newFile := c.quiet("file", "")
	if newFile == "" && c.interactive && !c.yes {
		if newFile, err = ask("Replace the file (path, empty to keep)", ""); err != nil {
			return err
		}
	}
	if newFile != "" {
		parts = append(parts, FFile("document", expandPath(newFile)))
	}
	if err := c.api.Multipart("/api/administrative-document/save", nil, parts, nil); err != nil {
		return err
	}
	info("document saved")
	return nil
}

func docDownload(c *Ctx) error {
	d, err := pickDoc(c)
	if err != nil {
		return err
	}
	return downloadAll(c, []string{str(d, "attachmentId")})
}

func docDelete(c *Ctx) error {
	d, err := pickDoc(c)
	if err != nil {
		return err
	}
	if err := c.confirmAction("Delete " + str(d, "title") + "?"); err != nil {
		return err
	}
	if err := c.api.Delete("/api/administrative-document", Q("id", str(d, "id")), nil); err != nil {
		return err
	}
	info("deleted")
	return nil
}
