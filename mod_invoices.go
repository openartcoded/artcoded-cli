package main

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func init() {
	register(&Module{
		Name: "invoices", Aliases: []string{"invoice", "inv"}, Title: "Invoices", Routes: []string{"invoice"},
		Help: "invoices: create, generate, download, peppol, credit notes",
		Actions: []*Action{
			{Name: "list", Aliases: []string{"ls"}, Help: "invoices (latest first)", Usage: "[--archived --deleted --bookmarked --page n --size n]", Run: invList},
			{Name: "show", Help: "an invoice", Usage: "<id>", Run: invShow},
			{Name: "new", Aliases: []string{"add", "create"}, Help: "create an invoice for a client (guided)", Usage: "[--client name --from <invoice id> --edit]", Run: invNew},
			{Name: "edit", Help: "edit an invoice not generated yet (json in the editor)", Usage: "<id>", Run: invEdit},
			{Name: "download", Aliases: []string{"pdf"}, Help: "pdf of the invoice (--xml: its peppol UBL)", Usage: "<id> [--xml --out path --open]", Run: invDownload},
			{Name: "upload", Help: "upload the pdf of an invoice made elsewhere", Usage: "<id> <file>", Run: invUpload},
			{Name: "attachments", Help: "files embedded in the peppol xml of a generated invoice", Usage: "<id> [--add file]... [--remove <upload id>]...", Run: invAttachments},
			{Name: "credit-note", Help: "make the credit note of an invoice", Usage: "<id>", Run: invCreditNote},
			{Name: "peppol-validate", Aliases: []string{"validate"}, Help: "validate the peppol xml", Usage: "<id>", Run: invPeppolValidate},
			{Name: "peppol-send", Aliases: []string{"send"}, Help: "send the invoice to peppol", Usage: "<id>", Run: invPeppolSend},
			{Name: "bookmark", Help: "bookmark / unbookmark", Usage: "<id>", Run: func(c *Ctx) error {
				return toggleBookmark(c, "/api/invoice/toggle-bookmarked", func() ([]M, error) { return searchInvoices(c, false, false, 0, 50) }, invDesc)
			}},
			{Name: "delete", Aliases: []string{"rm"}, Help: "delete an invoice (restorable, --physical: for good)", Usage: "<id> [--physical]", Run: invDelete},
			{Name: "restore", Help: "restore a deleted invoice", Usage: "<id>", Run: invRestore},
			{Name: "summaries", Aliases: []string{"summary"}, Help: "amounts invoiced per period and client", Run: invSummaries},
			{Name: "templates", Help: "invoice templates", Run: func(c *Ctx) error { c.args.flags["type"] = []string{"INVOICE"}; return tplList(c) }},
		},
	})
}

func searchInvoices(c *Ctx, archived, deleted bool, page, size int) ([]M, error) {
	var p M
	if err := c.api.Post("/api/invoice/page", Q("archived", archived, "logical", deleted, "page", page, "size", size, "sort", "dateCreation,DESC"), nil, &p); err != nil {
		return nil, err
	}
	return content(p), nil
}

func invNumber(i M) string {
	if n := str(i, "newInvoiceNumber"); n != "" {
		return n
	}
	return str(i, "invoiceNumber")
}

func invDesc(i M) string {
	kind := ""
	if boolean(i, "creditNote") {
		kind = yellow(" credit note")
	}
	return fmt.Sprintf("%s  %s  %s  %s%s", invNumber(i), fdate(i, "dateOfInvoice"), sens(str(i, "billTo.clientName")), moneyOf(i, "total"), kind)
}

func invStatus(i M) string {
	switch {
	case boolean(i, "logicalDelete"):
		return red("deleted")
	case boolean(i, "archived"):
		return dim("archived")
	case boolean(i, "locked"):
		return green("generated")
	default:
		return yellow("draft")
	}
}

func invList(c *Ctx) error {
	page, size, err := c.pageParams(20)
	if err != nil {
		return err
	}
	var p M
	if c.boolFlag("bookmarked") {
		err = c.api.Get("/api/invoice/bookmarked", Q("page", page, "size", size, "sort", "bookmarkedDate,DESC"), &p)
	} else {
		err = c.api.Post("/api/invoice/page", Q("archived", c.boolFlag("archived"), "logical", c.boolFlag("deleted"), "page", page, "size", size, "sort", "dateCreation,DESC"), nil, &p)
	}
	if err != nil {
		return err
	}
	return c.result(p, func() {
		switch {
		case c.boolFlag("bookmarked"):
			fmt.Fprintln(c.out, bold("Bookmarked invoices"))
		case c.boolFlag("deleted"):
			fmt.Fprintln(c.out, bold("Deleted invoices")+dim(" (restore: invoices restore <id>)"))
		case c.boolFlag("archived"):
			fmt.Fprintln(c.out, bold("Archived invoices")+dim(" (in a dossier) · the others: without --archived"))
		default:
			fmt.Fprintln(c.out, bold("Invoices")+dim(" (in no dossier yet) · the ones in a dossier: --archived"))
		}
		rows := [][]string{}
		for _, i := range content(p) {
			rows = append(rows, []string{invNumber(i), fdate(i, "dateOfInvoice"), sens(str(i, "billTo.clientName")), moneyOf(i, "subTotal"), moneyOf(i, "total"),
				invStatus(i) + mark(boolean(i, "creditNote"), yellow(" CN")), strings.ToLower(str(i, "peppolStatus")), mark(boolean(i, "bookmarked"), "★"), dim(str(i, "id"))})
		}
		table(c.out, []string{"number", "date", "client", "excl. vat", "total", "", "peppol", "", "id"}, rows)
		fmt.Fprintln(c.out, dim(pageInfo(p)))
	})
}

func pickInvoice(c *Ctx) (M, error) {
	m, err := c.pick("invoice", func() ([]M, error) {
		a, err := searchInvoices(c, false, false, 0, 50)
		if err != nil {
			return nil, err
		}
		b, _ := searchInvoices(c, true, false, 0, 50)
		return append(a, b...), nil
	}, invDesc)
	if err != nil {
		return nil, err
	}
	var i M
	if err := c.api.Post("/api/invoice/find-by-id", Q("id", str(m, "id")), nil, &i); err != nil {
		return nil, err
	}
	return i, nil
}

func invShow(c *Ctx) error {
	i, err := pickInvoice(c)
	if err != nil {
		return err
	}
	return c.result(i, func() {
		title := "Invoice"
		if boolean(i, "creditNote") {
			title = "Credit note"
		}
		details(c.out, title, bold(invNumber(i)), "status", invStatus(i), "date", fdate(i, "dateOfInvoice"), "due", fdate(i, "dueDate"),
			"client", sens(str(i, "billTo.clientName")), "vat number", sens(str(i, "billTo.vatNumber")), "address", sens(strings.TrimSpace(str(i, "billTo.address")+", "+str(i, "billTo.city"))),
			"email", sens(str(i, "billTo.emailAddress")), "structured ref", str(i, "structuredReference"),
			"excl. vat", moneyOf(i, "subTotal"), "vat", moneyOf(i, "taxes")+" ("+str(i, "taxRate")+"%)", "total", bold(moneyOf(i, "total")),
			"peppol", str(i, "peppolStatus"), "credit note of", str(i, "creditNoteInvoiceReference"), "note", str(i, "specialNote"), "id", str(i, "id"))
		rows := [][]string{}
		for _, r := range maps(i["invoiceTable"]) {
			rows = append(rows, []string{str(r, "period"), str(r, "projectName"), str(r, "nature"), str(r, "amount") + " " + strings.ToLower(str(r, "amountType")),
				money(num(r, "rate")) + "/" + strings.ToLower(str(r, "rateType")), moneyOf(r, "total")})
		}
		fmt.Fprintln(c.out)
		table(c.out, []string{"period", "project", "nature", "amount", "rate", "total"}, rows)
		printUploads(c, "Extra files", strList(i, "attachmentIds"))
	})
}

func loadClients(c *Ctx) ([]M, error) {
	var l []M
	return l, c.api.Get("/api/billable-client/find-all", nil, &l)
}

func clientRatesOf(cl M) []M {
	if r := maps(cl["rates"]); len(r) > 0 {
		return r
	}
	if get(cl, "rate") != nil {
		rt := str(cl, "rateType")
		if rt == "" {
			rt = "DAYS"
		}
		return []M{{"id": "default", "rate": num(cl, "rate"), "rateType": rt}}
	}
	return nil
}

func invNew(c *Ctx) error {
	var inv M
	if from, ok := c.flag("from"); ok {
		if err := c.api.Post("/api/invoice/from-template", Q("id", from), nil, &inv); err != nil {
			return err
		}
	} else if err := c.api.Post("/api/invoice/new", nil, nil, &inv); err != nil {
		return err
	}
	// the client fills the "bill to" part and the rows
	clients, err := loadClients(c)
	if err != nil {
		return err
	}
	var client M
	if name, ok := c.flag("client"); ok {
		for _, cl := range clients {
			if strings.Contains(strings.ToLower(str(cl, "name")), strings.ToLower(name)) {
				client = cl
				break
			}
		}
		if client == nil {
			return fmt.Errorf("no client matching %q", name)
		}
	} else if c.interactive && !c.yes && len(maps(inv["invoiceTable"])) == 0 {
		var ongoing []M
		for _, cl := range clients {
			if str(cl, "contractStatus") != "DONE" {
				ongoing = append(ongoing, cl)
			}
		}
		opts := make([]string, len(ongoing))
		for i, cl := range ongoing {
			opts[i] = str(cl, "name") + dim("  "+str(cl, "projectName"))
		}
		if len(opts) > 0 {
			i, err := choose("Client", opts)
			if err != nil {
				return err
			}
			client = ongoing[i]
		}
	}
	if client != nil {
		inv["billTo"] = M{"vatNumber": str(client, "vatNumber"), "clientName": str(client, "name"), "city": str(client, "city"),
			"address": str(client, "address"), "emailAddress": str(client, "emailAddress")}
		if get(client, "maxDaysToPay") != nil {
			inv["maxDaysToPay"] = num(client, "maxDaysToPay")
		}
		if get(client, "taxRate") != nil {
			inv["taxRate"] = num(client, "taxRate")
		}
	}
	if len(maps(inv["invoiceTable"])) == 0 {
		rows, err := askInvoiceRows(c, client)
		if err != nil {
			return err
		}
		inv["invoiceTable"] = rows
	}
	d, err := c.date("date", "Date of the invoice", "today", true)
	if err != nil {
		return err
	}
	inv["dateOfInvoice"] = iso(*d)
	if get(inv, "taxRate") == nil {
		tr, err := c.num("tax-rate", "VAT rate (%)", ptr(21.0), true)
		if err != nil {
			return err
		}
		inv["taxRate"] = *tr
	}
	if str(inv, "freemarkerTemplateId") == "" {
		var def M
		if err := c.api.Get("/api/template/default", Q("type", "INVOICE"), &def); err == nil && def != nil {
			inv["freemarkerTemplateId"] = str(def, "id")
		}
	}
	if note, err := c.opt("note", "Special note (optional)", str(inv, "specialNote")); err != nil {
		return err
	} else if note != "" {
		inv["specialNote"] = note
	}
	if c.boolFlag("edit") || c.interactive && !c.yes && askBool("Review the invoice in the editor before saving?", false) {
		var edited M
		if _, err := editJSON(c.cfg, inv, &edited); err != nil {
			return err
		}
		if edited != nil {
			inv = edited
		}
	}
	var saved M
	if err := c.api.Post("/api/invoice/save", nil, inv, &saved); err != nil {
		return err
	}
	return c.result(saved, func() {
		info("invoice saved, the pdf is generated in the background (%s)", str(saved, "id"))
		fmt.Fprintln(c.out, dim("  artcoded invoices download "+str(saved, "id")))
	})
}

func ptr[T any](v T) *T { return &v }

func askBool(label string, def bool) bool {
	b, err := confirm(label, def)
	return err == nil && b
}

func askInvoiceRows(c *Ctx, client M) ([]M, error) {
	rates := clientRatesOf(client)
	period := time.Now().In(brussels).Format("01/2006")
	var rows []M
	for n := 1; ; n++ {
		row := M{"projectName": str(client, "projectName"), "nature": str(client, "nature"), "period": period, "amountType": "DAYS", "rateType": "DAYS"}
		if len(rates) > 0 {
			r := rates[0]
			if len(rates) > 1 && c.interactive && !c.has("rate") {
				opts := make([]string, len(rates))
				for i, x := range rates {
					opts[i] = fmt.Sprintf("%s %s/%s", str(x, "label"), money(num(x, "rate")), strings.ToLower(str(x, "rateType")))
				}
				i, err := choose(fmt.Sprintf("Rate of row %d", n), opts)
				if err != nil {
					return nil, err
				}
				r = rates[i]
			}
			row["rate"] = num(r, "rate")
			row["rateType"] = str(r, "rateType")
			if l := str(r, "label"); l != "" && str(row, "nature") != "" {
				row["nature"] = str(row, "nature") + " - " + l
			}
		}
		prefix := fmt.Sprintf("Row %d: ", n)
		var err error
		if row["projectName"], err = c.req("project", prefix+"project", str(row, "projectName")); err != nil {
			return nil, err
		}
		if row["nature"], err = c.req("nature", prefix+"nature", str(row, "nature")); err != nil {
			return nil, err
		}
		if row["period"], err = c.req("period", prefix+"period (MM/YYYY)", period); err != nil {
			return nil, err
		}
		if row["amountType"], err = c.enum("amount-type", prefix+"amount in", []string{"DAYS", "HOURS"}, str(row, "amountType")); err != nil {
			return nil, err
		}
		amount, err := c.num("amount", prefix+"amount ("+strings.ToLower(str(row, "amountType"))+")", nil, true)
		if err != nil {
			return nil, err
		}
		row["amount"] = *amount
		var curRate *float64
		if get(row, "rate") != nil {
			curRate = ptr(num(row, "rate"))
		}
		rate, err := c.num("rate", prefix+"rate", curRate, true)
		if err != nil {
			return nil, err
		}
		row["rate"] = *rate
		if row["rateType"], err = c.enum("rate-type", prefix+"rate per", []string{"DAYS", "HOURS"}, str(row, "rateType")); err != nil {
			return nil, err
		}
		if str(row, "amountType") != str(row, "rateType") {
			h, err := c.num("hours-per-day", prefix+"hours per day", ptr(8.0), true)
			if err != nil {
				return nil, err
			}
			row["hoursPerDay"] = *h
		}
		rows = append(rows, row)
		if !c.interactive || c.yes || c.has("amount") || !askBool("Add another row?", false) {
			return rows, nil
		}
	}
}

func invEdit(c *Ctx) error {
	i, err := pickInvoice(c)
	if err != nil {
		return err
	}
	if boolean(i, "locked") {
		warn("this invoice is generated: the backend may refuse changes (make a credit note instead)")
	}
	var edited M
	changed, err := editJSON(c.cfg, i, &edited)
	if err != nil || !changed {
		if err == nil {
			info("no change")
		}
		return err
	}
	if err := c.api.Post("/api/invoice/save", nil, edited, nil); err != nil {
		return err
	}
	info("invoice saved")
	return nil
}

func invDownload(c *Ctx) error {
	i, err := pickInvoice(c)
	if err != nil {
		return err
	}
	id := str(i, "invoiceUploadId")
	if c.boolFlag("xml") {
		id = str(i, "invoiceUBLId")
	}
	if id == "" {
		return errors.New("no file yet (the pdf is generated in the background after a save)")
	}
	d, err := c.api.FileDownload(id)
	if err != nil {
		return err
	}
	return c.save(d)
}

func invUpload(c *Ctx) error {
	i, err := pickInvoice(c)
	if err != nil {
		return err
	}
	path, err := c.file("file", "Pdf of the invoice")
	if err != nil {
		return err
	}
	if err := c.api.Multipart("/api/invoice/manual-upload", nil, []Part{F("id", str(i, "id")), FFile("manualUploadFile", path)}, nil); err != nil {
		return err
	}
	info("uploaded")
	return nil
}

func invAttachments(c *Ctx) error {
	i, err := pickInvoice(c)
	if err != nil {
		return err
	}
	ids := strList(i, "attachmentIds")
	adds, removes := c.flags("add"), c.flags("remove")
	if len(adds) == 0 && len(removes) == 0 {
		if !c.interactive {
			printUploads(c, "Extra files", ids)
			return nil
		}
		printUploads(c, "Extra files", ids)
		op, err := c.enum("op", "Change", []string{"add files", "remove a file", "nothing"}, "nothing")
		if err != nil {
			return err
		}
		switch op {
		case "add files":
			if adds, err = c.files("add", "File", true); err != nil {
				return err
			}
		case "remove a file":
			rm, err := chooseUpload(c, ids)
			if err != nil {
				return err
			}
			removes = []string{rm}
		default:
			return nil
		}
	}
	for _, p := range adds {
		up, err := c.api.Upload(expandPath(p), str(i, "id"), false)
		if err != nil {
			return err
		}
		ids = append(ids, str(up, "id"))
	}
	var keep []string
	for _, id := range ids {
		drop := false
		for _, r := range removes {
			drop = drop || r == id
		}
		if !drop {
			keep = append(keep, id)
		}
	}
	if keep == nil {
		keep = []string{}
	}
	var saved M
	if err := c.api.Post("/api/invoice/update-attachments", Q("id", str(i, "id")), keep, &saved); err != nil {
		return err
	}
	info("attachments saved (%d file(s))", len(keep))
	return nil
}

func invCreditNote(c *Ctx) error {
	i, err := pickInvoice(c)
	if err != nil {
		return err
	}
	if err := c.confirmAction("Make the credit note of " + invNumber(i) + "?"); err != nil {
		return err
	}
	var cn M
	if err := c.api.Post("/api/invoice/make-credit-note", Q("id", str(i, "id")), nil, &cn); err != nil {
		return err
	}
	return c.result(cn, func() { info("credit note created (%s)", str(cn, "id")) })
}

func invPeppolValidate(c *Ctx) error {
	i, err := pickInvoice(c)
	if err != nil {
		return err
	}
	var r M
	if err := c.api.Post("/api/invoice/validate-peppol", Q("id", str(i, "id")), nil, &r); err != nil {
		return err
	}
	return c.result(r, func() {
		if boolean(r, "valid") {
			info("valid peppol xml")
		} else {
			fmt.Fprintln(c.out, red("invalid peppol xml"))
		}
		if s := strings.TrimSpace(str(r, "results")); s != "" {
			fmt.Fprintln(c.out, s)
		}
	})
}

func invPeppolSend(c *Ctx) error {
	i, err := pickInvoice(c)
	if err != nil {
		return err
	}
	if err := c.confirmAction("Send " + invNumber(i) + " to peppol?"); err != nil {
		return err
	}
	if err := c.api.Post("/api/invoice/send-to-peppol", Q("id", str(i, "id")), nil, nil); err != nil {
		return err
	}
	info("sent to peppol")
	return nil
}

func invDelete(c *Ctx) error {
	i, err := pickInvoice(c)
	if err != nil {
		return err
	}
	physical := c.boolFlag("physical")
	q := "Delete " + invNumber(i) + "? (it can be restored)"
	if physical {
		q = "Delete " + invNumber(i) + " for good?"
	}
	if err := c.confirmAction(q); err != nil {
		return err
	}
	if err := c.api.do(http.MethodDelete, "/api/invoice", Q("id", str(i, "id"), "logical", !physical), nil, nil); err != nil {
		return err
	}
	info("deleted")
	return nil
}

func invRestore(c *Ctx) error {
	id, err := c.pickID("deleted invoice", func() ([]M, error) {
		a, err := searchInvoices(c, false, true, 0, 50)
		if err != nil {
			return nil, err
		}
		b, _ := searchInvoices(c, true, true, 0, 50)
		return append(a, b...), nil
	}, invDesc)
	if err != nil {
		return err
	}
	if err := c.api.Post("/api/invoice/restore", Q("id", id), nil, nil); err != nil {
		return err
	}
	info("restored")
	return nil
}

func invSummaries(c *Ctx) error {
	var l []M
	if err := c.api.Post("/api/invoice/find-all-summaries", nil, nil, &l); err != nil {
		return err
	}
	return c.result(l, func() {
		rows := [][]string{}
		var total float64
		for _, s := range l {
			total += num(s, "subTotal")
			rows = append(rows, []string{fdate(s, "dateOfInvoice"), str(s, "period"), sens(str(s, "client")), str(s, "amount") + " " + strings.ToLower(str(s, "amountType")), moneyOf(s, "subTotal")})
		}
		rows = append(rows, []string{"", "", bold("total"), "", bold(money(total))})
		table(c.out, []string{"date", "period", "client", "amount", "excl. vat"}, rows)
	})
}
