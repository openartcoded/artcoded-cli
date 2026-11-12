package main

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
)

func init() {
	register(&Module{
		Name: "expenses", Aliases: []string{"fee", "fees", "expense"}, Title: "Expenses", Routes: []string{"fee"},
		Help: "expenses: add from files, tag, price, attachments",
		Actions: []*Action{
			{Name: "list", Aliases: []string{"ls", "search"}, Help: "search the expenses", Usage: "[--processed --bookmarked --tag T --subject s --from date --to date --proof|--no-proof --page n --size n]", Run: feeList},
			{Name: "show", Help: "an expense and its attachments", Usage: "<id>", Run: feeShow},
			{Name: "add", Aliases: []string{"new", "upload"}, Help: "add an expense from files (invoice, receipt...)", Usage: "<file...> [--subject s --body b --tag T --price 100 --vat 21]", Run: feeAdd},
			{Name: "tag", Help: "set the tag (label) of expenses", Usage: "<id...> --tag T", Run: feeTag},
			{Name: "price", Help: "set the price of an expense", Usage: "<id> --price 100 --vat 21", Run: feePrice},
			{Name: "attach", Help: "add a file to an expense", Usage: "<id> <file> [--payment-proof]", Run: feeAttach},
			{Name: "detach", Help: "remove a file of an expense", Usage: "<id> [--attachment <upload id>]", Run: feeDetach},
			{Name: "download", Aliases: []string{"files"}, Help: "download the files of an expense", Usage: "<id> [--out dir --open]", Run: feeDownload},
			{Name: "edit", Help: "edit an expense (json in the editor)", Usage: "<id>", Run: feeEdit},
			{Name: "bookmark", Help: "bookmark / unbookmark", Usage: "<id>", Run: func(c *Ctx) error {
				return toggleBookmark(c, "/api/fee/toggle-bookmarked", func() ([]M, error) { return searchFees(c, M{"archived": false}, 0, 100) }, feeDesc)
			}},
			{Name: "delete", Aliases: []string{"rm"}, Help: "delete expenses", Usage: "<id...>", Run: feeDelete},
			{Name: "summaries", Aliases: []string{"summary"}, Help: "totals per tag", Run: feeSummaries},
			{Name: "labels", Help: "the tags (labels) with their default price", Run: labelList},
			{Name: "labels-edit", Help: "edit the labels (json in the editor)", Run: labelEdit},
			{Name: "exceptional", Help: "expenses far above the usual ones", Usage: "[--factor 3 --min 1000]", Run: feeExceptional},
			{Name: "review", Help: "mark expenses exceptional (CONFIRMED), normal (DENIED) or not reviewed (NONE)", Usage: "<id...> --status CONFIRMED|DENIED|NONE", Run: feeReview},
		},
	})
}

func searchFees(c *Ctx, criteria M, page, size int) ([]M, error) {
	var p M
	if err := c.api.Post("/api/fee/search", Q("page", page, "size", size, "sort", "date,DESC"), criteria, &p); err != nil {
		return nil, err
	}
	return content(p), nil
}

func feeDesc(f M) string {
	s := fmt.Sprintf("%s  %s", fdate(f, "date"), truncate(str(f, "subject"), 50))
	if t := str(f, "tag"); t != "" {
		s += "  " + cyan(t)
	}
	if get(f, "priceTot") != nil {
		s += "  " + moneyOf(f, "priceTot")
	}
	return s
}

func feeCriteria(c *Ctx) (M, error) {
	cr := M{"archived": c.boolFlag("processed") || c.boolFlag("archived"), "bookmarked": c.boolFlag("bookmarked")}
	for flag, field := range map[string]string{"tag": "tag", "subject": "subject", "body": "body", "id": "id"} {
		if v, ok := c.flag(flag); ok {
			cr[field] = v
		}
	}
	for flag, field := range map[string]string{"from": "dateAfter", "to": "dateBefore"} {
		if v, ok := c.flag(flag); ok {
			t, err := parseDate(v)
			if err != nil {
				return nil, err
			}
			cr[field] = iso(t)
		}
	}
	if c.has("proof") {
		cr["hasPaymentProof"] = c.boolFlag("proof")
	}
	return cr, nil
}

func feeList(c *Ctx) error {
	cr, err := feeCriteria(c)
	if err != nil {
		return err
	}
	page, size, err := c.pageParams(30)
	if err != nil {
		return err
	}
	var p M
	if err := c.api.Post("/api/fee/search", Q("page", page, "size", size, "sort", "date,DESC"), cr, &p); err != nil {
		return err
	}
	return c.result(p, func() {
		// the backend lists one of: the bookmarked ones, the processed ones (in a dossier), the ones to process
		switch {
		case boolean(cr, "bookmarked"):
			fmt.Fprintln(c.out, bold("Bookmarked expenses"))
		case boolean(cr, "archived"):
			fmt.Fprintln(c.out, bold("Processed expenses")+dim(" (in a dossier) · to process: without --processed"))
		default:
			fmt.Fprintln(c.out, bold("Expenses to process")+dim(" (in no dossier yet) · the others: --processed"))
		}
		rows := [][]string{}
		for _, f := range content(p) {
			rows = append(rows, []string{fdate(f, "date"), str(f, "subject"), str(f, "tag"), moneyOf(f, "priceHVAT"), moneyOf(f, "vat"), moneyOf(f, "priceTot"),
				fmt.Sprint(len(list(f, "attachmentIds"))), mark(str(f, "paymentProofUploadId") != "", "✓") + mark(boolean(f, "bookmarked"), "★"), dim(str(f, "id"))})
		}
		table(c.out, []string{"date", "subject", "tag", "excl. vat", "vat", "total", "files", "proof", "id"}, rows)
		fmt.Fprintln(c.out, dim(pageInfo(p)))
		if len(rows) == 0 && !boolean(cr, "archived") && !boolean(cr, "bookmarked") {
			fmt.Fprintln(c.out, dim("nothing left to process: the expenses already in a dossier are listed with --processed"))
		}
	})
}

func pickFee(c *Ctx) (M, error) {
	m, err := c.pick("expense", func() ([]M, error) { return searchFees(c, M{"archived": false}, 0, 100) }, feeDesc)
	if err != nil {
		return nil, err
	}
	var f M
	if err := c.api.Post("/api/fee/find-by-id", Q("id", str(m, "id")), nil, &f); err != nil {
		return nil, err
	}
	return f, nil
}

func uploadsByIDs(c *Ctx, ids []string) ([]M, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var l []M
	return l, c.api.Get("/api/resource/find-by-ids", Q("id", ids), &l)
}

func printUploads(c *Ctx, title string, ids []string) {
	ups, err := uploadsByIDs(c, ids)
	if err != nil || len(ups) == 0 {
		return
	}
	fmt.Fprintln(c.out, "\n"+bold(title))
	rows := [][]string{}
	for _, u := range ups {
		rows = append(rows, []string{str(u, "originalFilename"), str(u, "contentType"), humanSize(num(u, "size")), dim(str(u, "id"))})
	}
	table(c.out, []string{"file", "type", "size", "id"}, rows)
}

func feeShow(c *Ctx) error {
	f, err := pickFee(c)
	if err != nil {
		return err
	}
	return c.result(f, func() {
		details(c.out, "subject", bold(str(f, "subject")), "date", fdate(f, "date"), "tag", str(f, "tag"),
			"excl. vat", moneyOf(f, "priceHVAT"), "vat", moneyOf(f, "vat"), "total", moneyOf(f, "priceTot"),
			"archived", mark(boolean(f, "archived"), "yes (in a dossier)"), "exceptional", str(f, "exceptional"), "id", str(f, "id"))
		if b := strings.TrimSpace(str(f, "body")); b != "" {
			fmt.Fprintln(c.out, "\n"+b)
		}
		printUploads(c, "Files", strList(f, "attachmentIds"))
		if p := str(f, "paymentProofUploadId"); p != "" {
			printUploads(c, "Payment proof", []string{p})
		}
	})
}

func feeAdd(c *Ctx) error {
	files, err := c.files("file", "File", true)
	if err != nil {
		return err
	}
	def := strings.TrimSuffix(filepath.Base(files[0]), filepath.Ext(files[0]))
	subject, err := c.req("subject", "Subject", def)
	if err != nil {
		return err
	}
	body, err := c.opt("body", "Description", "")
	if err != nil {
		return err
	}
	parts := []Part{F("subject", subject), F("body", body)}
	for _, f := range files {
		parts = append(parts, FFile("files", f))
	}
	var fee M
	if err := c.api.Multipart("/api/fee/manual-submit", nil, parts, &fee); err != nil {
		return err
	}
	info("expense added: %s (%s)", subject, str(fee, "id"))
	// tag and price right away
	if err := setFeeTag(c, []string{str(fee, "id")}, false); err != nil && !errors.Is(err, errCancel) {
		return err
	}
	if c.has("price") || c.interactive && !c.yes {
		if err := setFeePrice(c, str(fee, "id"), nil, nil, false); err != nil && !errors.Is(err, errCancel) {
			return err
		}
	}
	return nil
}

func loadLabels(c *Ctx) ([]M, error) {
	var l []M
	return l, c.api.Post("/api/label/find-all", nil, nil, &l)
}

// setFeeTag: --tag, or chosen from the labels (optional when not required). Applies the default price of the label.
func setFeeTag(c *Ctx, ids []string, required bool) error {
	tag, ok := c.flag("tag")
	labels, err := loadLabels(c)
	if err != nil {
		return err
	}
	if !ok {
		if !c.interactive || c.yes {
			if required {
				return missing("tag")
			}
			return nil
		}
		var visible []M
		opts := []string{}
		for _, l := range labels {
			if boolean(l, "hidden") {
				continue
			}
			visible = append(visible, l)
			s := str(l, "name")
			if !boolean(l, "noDefaultPrice") && get(l, "priceHVAT") != nil {
				s += dim(fmt.Sprintf("  %s + %s vat", moneyOf(l, "priceHVAT"), moneyOf(l, "vat")))
			}
			opts = append(opts, s)
		}
		if !required {
			opts = append(opts, dim("(no tag for now)"))
		}
		i, err := choose("Tag", opts)
		if err != nil {
			return err
		}
		if i == len(visible) {
			return nil
		}
		tag = str(visible[i], "name")
	}
	if err := c.api.Post("/api/fee/update-tag", Q("tag", tag), ids, nil); err != nil {
		return err
	}
	info("tagged %s", tag)
	return nil
}

func setFeePrice(c *Ctx, id string, curH, curV *float64, required bool) error {
	h, err := c.num("price", "Price excl. VAT (empty to skip)", curH, required)
	if err != nil || h == nil {
		return err
	}
	v, err := c.num("vat", "VAT amount", curV, true)
	if err != nil {
		return err
	}
	if err := c.api.Post("/api/fee/update-price", Q("id", id, "priceHVat", *h, "vat", *v), nil, nil); err != nil {
		return err
	}
	info("price %s + %s vat", money(*h), money(*v))
	return nil
}

func feeTag(c *Ctx) error {
	ids, err := c.pickMany("expenses", func() ([]M, error) { return searchFees(c, M{"archived": false}, 0, 100) }, feeDesc)
	if err != nil {
		return err
	}
	return setFeeTag(c, ids, true)
}

func feePrice(c *Ctx) error {
	f, err := pickFee(c)
	if err != nil {
		return err
	}
	var h, v *float64
	if get(f, "priceHVAT") != nil {
		x := num(f, "priceHVAT")
		h = &x
	}
	if get(f, "vat") != nil {
		x := num(f, "vat")
		v = &x
	}
	return setFeePrice(c, str(f, "id"), h, v, true)
}

func feeAttach(c *Ctx) error {
	f, err := pickFee(c)
	if err != nil {
		return err
	}
	path, err := c.file("file", "File")
	if err != nil {
		return err
	}
	proof, err := c.boolOpt("payment-proof", "Is it the payment proof?", false)
	if err != nil {
		return err
	}
	if err := c.api.Multipart("/api/fee/add-attachment", nil, []Part{F("id", str(f, "id")), F("isPaymentProof", fmt.Sprint(proof)), FFile("file", path)}, nil); err != nil {
		return err
	}
	info("%s attached", filepath.Base(path))
	return nil
}

// chooseUpload: --attachment or chosen among ids
func chooseUpload(c *Ctx, ids []string) (string, error) {
	if v, ok := c.flag("attachment"); ok {
		return v, nil
	}
	if len(ids) == 0 {
		return "", errors.New("no file")
	}
	if !c.interactive {
		return "", missing("attachment")
	}
	ups, err := uploadsByIDs(c, ids)
	if err != nil {
		return "", err
	}
	opts := make([]string, len(ups))
	for i, u := range ups {
		opts[i] = str(u, "originalFilename") + dim("  "+humanSize(num(u, "size")))
	}
	i, err := choose("File", opts)
	if err != nil {
		return "", err
	}
	return str(ups[i], "id"), nil
}

func feeDetach(c *Ctx) error {
	f, err := pickFee(c)
	if err != nil {
		return err
	}
	ids := strList(f, "attachmentIds")
	if p := str(f, "paymentProofUploadId"); p != "" {
		ids = append(ids, p)
	}
	att, err := chooseUpload(c, ids)
	if err != nil {
		return err
	}
	if err := c.confirmAction("Remove this file?"); err != nil {
		return err
	}
	if err := c.api.Post("/api/fee/remove-attachment", Q("id", str(f, "id"), "attachmentId", att), nil, nil); err != nil {
		return err
	}
	info("removed")
	return nil
}

// downloadAll: each file (--out must then be a directory)
func downloadAll(c *Ctx, ids []string) error {
	if len(ids) == 0 {
		return errors.New("no file")
	}
	for _, id := range ids {
		d, err := c.api.FileDownload(id)
		if err != nil {
			return err
		}
		if err := c.save(d); err != nil {
			return err
		}
	}
	return nil
}

func feeDownload(c *Ctx) error {
	f, err := pickFee(c)
	if err != nil {
		return err
	}
	ids := strList(f, "attachmentIds")
	if p := str(f, "paymentProofUploadId"); p != "" {
		ids = append(ids, p)
	}
	return downloadAll(c, ids)
}

func feeEdit(c *Ctx) error {
	f, err := pickFee(c)
	if err != nil {
		return err
	}
	var edited M
	changed, err := editJSON(c.cfg, f, &edited)
	if err != nil || !changed {
		if err == nil {
			info("no change")
		}
		return err
	}
	if err := c.api.Post("/api/fee/update", nil, edited, nil); err != nil {
		return err
	}
	info("expense saved")
	return nil
}

func feeDelete(c *Ctx) error {
	ids, err := c.pickMany("expenses", func() ([]M, error) { return searchFees(c, M{"archived": false}, 0, 100) }, feeDesc)
	if err != nil {
		return err
	}
	if err := c.confirmAction(fmt.Sprintf("Delete %d expense(s)?", len(ids))); err != nil {
		return err
	}
	for _, id := range ids {
		if err := c.api.Delete("/api/fee", Q("id", id), nil); err != nil {
			return err
		}
	}
	info("deleted")
	return nil
}

func feeSummaries(c *Ctx) error {
	var l []M
	if err := c.api.Post("/api/fee/summaries", nil, nil, &l); err != nil {
		return err
	}
	return c.result(l, func() {
		rows := [][]string{}
		var th, tv float64
		for _, s := range l {
			th += num(s, "totalHVAT")
			tv += num(s, "totalVAT")
			rows = append(rows, []string{str(s, "tag"), moneyOf(s, "totalHVAT"), moneyOf(s, "totalVAT")})
		}
		rows = append(rows, []string{bold("total"), bold(money(th)), bold(money(tv))})
		table(c.out, []string{"tag", "excl. vat", "vat"}, rows)
	})
}

func labelList(c *Ctx) error {
	l, err := loadLabels(c)
	if err != nil {
		return err
	}
	return c.result(l, func() {
		rows := [][]string{}
		for _, x := range l {
			price := ""
			if !boolean(x, "noDefaultPrice") && get(x, "priceHVAT") != nil {
				price = moneyOf(x, "priceHVAT") + " + " + moneyOf(x, "vat")
			}
			rows = append(rows, []string{str(x, "name"), price, str(x, "colorHex"), mark(boolean(x, "hidden"), "hidden"), str(x, "description")})
		}
		table(c.out, []string{"label", "default price", "color", "", "description"}, rows)
	})
}

func labelEdit(c *Ctx) error {
	l, err := loadLabels(c)
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
	if err := c.api.Post("/api/label/update-all", nil, edited, nil); err != nil {
		return err
	}
	info("labels saved")
	return nil
}

func feeExceptional(c *Ctx) error {
	q := Q("factor", c.quiet("factor", ""), "minAmount", c.quiet("min", ""))
	var r M
	if err := c.api.Post("/api/fee/exceptional", q, nil, &r); err != nil {
		return err
	}
	return c.result(r, func() {
		details(c.out, "median", moneyOf(r, "median"), "threshold", moneyOf(r, "threshold"), "factor", str(r, "factor"), "min amount", moneyOf(r, "minAmount"))
		rows := [][]string{}
		for _, f := range maps(r["expenses"]) {
			rows = append(rows, []string{fdate(f, "date"), str(f, "subject"), str(f, "tag"), moneyOf(f, "priceTot"), str(f, "exceptional"), dim(str(f, "id"))})
		}
		fmt.Fprintln(c.out)
		table(c.out, []string{"date", "subject", "tag", "total", "review", "id"}, rows)
	})
}

func feeReview(c *Ctx) error {
	ids, err := c.pickMany("expenses", func() ([]M, error) {
		var r M
		if err := c.api.Post("/api/fee/exceptional", nil, nil, &r); err != nil {
			return nil, err
		}
		return maps(r["expenses"]), nil
	}, feeDesc)
	if err != nil {
		return err
	}
	status, err := c.enum("status", "Review", []string{"CONFIRMED", "DENIED", "NONE"}, "")
	if err != nil {
		return err
	}
	if err := c.api.Post("/api/fee/update-exceptional", Q("status", status), ids, nil); err != nil {
		return err
	}
	info("%d expense(s) %s", len(ids), strings.ToLower(status))
	return nil
}

// toggleBookmark: shared by the modules with a toggle-bookmarked route
func toggleBookmark(c *Ctx, path string, load func() ([]M, error), describe func(M) string) error {
	id, err := c.pickID("item", load, describe)
	if err != nil {
		return err
	}
	var r M
	if err := c.api.do(http.MethodPost, path, Q("id", id), nil, &r); err != nil {
		return err
	}
	if r != nil && get(r, "bookmarked") != nil {
		info("bookmarked: %s", yesNo(boolean(r, "bookmarked")))
	} else {
		info("bookmark toggled")
	}
	return nil
}
