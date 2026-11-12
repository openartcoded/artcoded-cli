package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

func init() {
	register(&Module{
		Name: "clients", Aliases: []string{"client", "billable-clients"}, Title: "Clients", Routes: []string{"billable-clients"},
		Help: "billable clients, rates, contracts, documents",
		Actions: []*Action{
			{Name: "list", Aliases: []string{"ls"}, Help: "clients", Usage: "[--status NOT_STARTED_YET|ONGOING|DONE]", Run: clientList},
			{Name: "show", Help: "a client", Usage: "<id>", Run: clientShow},
			{Name: "add", Aliases: []string{"new", "create"}, Help: "add a client (guided)", Usage: "[--name .. --vat .. --email .. --rate 600 --rate-type DAYS ...]", Run: clientAdd},
			{Name: "edit", Help: "edit a client (json in the editor)", Usage: "<id>", Run: clientEdit},
			{Name: "status", Help: "change the contract status", Usage: "<id> --status ONGOING", Run: clientStatus},
			{Name: "upload", Help: "add a document (contract...) to a client", Usage: "<id> <file>", Run: clientUpload},
			{Name: "documents", Aliases: []string{"docs"}, Help: "documents of a client", Usage: "<id>", Run: clientDocs},
			{Name: "download", Help: "download a document of a client", Usage: "<id> [--attachment <upload id>]", Run: clientDownload},
			{Name: "remove-document", Help: "remove a document of a client", Usage: "<id> [--attachment <upload id>]", Run: clientRemoveDoc},
			{Name: "delete", Aliases: []string{"rm"}, Help: "delete a client", Usage: "<id>", Run: clientDelete},
		},
	})
	register(&Module{
		Name: "prospects", Aliases: []string{"contacts", "services"}, Title: "Prospects", Routes: []string{"services"},
		Help: "messages of the contact form",
		Actions: []*Action{
			{Name: "list", Aliases: []string{"ls"}, Help: "messages received", Run: prospectList},
			{Name: "show", Help: "a message", Usage: "<id>", Run: prospectShow},
			{Name: "delete", Aliases: []string{"rm"}, Help: "delete messages", Usage: "<id...>", Run: prospectDelete},
		},
	})
}

var contractStatuses = []string{"NOT_STARTED_YET", "ONGOING", "DONE"}

func clientDesc(cl M) string {
	return sens(str(cl, "name")) + dim("  "+str(cl, "projectName")+"  "+strings.ToLower(str(cl, "contractStatus")))
}

func clientList(c *Ctx) error {
	var l []M
	var err error
	if s, ok := c.flag("status"); ok {
		err = c.api.Get("/api/billable-client/find-by-contract-status", Q("contractStatus", strings.ToUpper(s)), &l)
	} else {
		l, err = loadClients(c)
	}
	if err != nil {
		return err
	}
	return c.result(l, func() {
		rows := [][]string{}
		for _, cl := range l {
			rate := ""
			if r := clientRatesOf(cl); len(r) > 0 {
				rate = fmt.Sprintf("%s/%s", money(num(r[0], "rate")), strings.ToLower(str(r[0], "rateType")))
				if len(r) > 1 {
					rate += fmt.Sprintf(" +%d", len(r)-1)
				}
			}
			rows = append(rows, []string{sens(str(cl, "name")), str(cl, "projectName"), rate, strings.ToLower(str(cl, "contractStatus")), fdate(cl, "startDate"), fdate(cl, "endDate"), dim(str(cl, "id"))})
		}
		table(c.out, []string{"client", "project", "rate", "status", "start", "end", "id"}, rows)
	})
}

func pickClient(c *Ctx) (M, error) {
	m, err := c.pick("client", func() ([]M, error) { return loadClients(c) }, clientDesc)
	if err != nil {
		return nil, err
	}
	if len(m) > 1 {
		return m, nil
	}
	l, err := loadClients(c)
	if err != nil {
		return nil, err
	}
	for _, cl := range l {
		if str(cl, "id") == str(m, "id") {
			return cl, nil
		}
	}
	return nil, fmt.Errorf("client %s not found", str(m, "id"))
}

func clientShow(c *Ctx) error {
	cl, err := pickClient(c)
	if err != nil {
		return err
	}
	return c.result(cl, func() {
		details(c.out, "client", bold(sens(str(cl, "name"))), "project", str(cl, "projectName"), "nature", str(cl, "nature"), "status", str(cl, "contractStatus"),
			"vat number", sens(str(cl, "vatNumber")), "address", sens(str(cl, "address")), "city", sens(str(cl, "city")), "country", str(cl, "countryCode"),
			"email", sens(str(cl, "emailAddress")), "phone", sens(str(cl, "phoneNumber")), "vat rate", str(cl, "taxRate"), "days to pay", str(cl, "maxDaysToPay"),
			"start", fdate(cl, "startDate"), "end", fdate(cl, "endDate"), "working days", strings.ToLower(str(cl, "defaultWorkingDays")),
			"student", mark(boolean(cl, "student"), "yes"), "id", str(cl, "id"))
		if r := clientRatesOf(cl); len(r) > 0 {
			fmt.Fprintln(c.out, "\n"+bold("Rates"))
			for i, x := range r {
				fmt.Fprintf(c.out, "  %s %s/%s%s\n", str(x, "label"), money(num(x, "rate")), strings.ToLower(str(x, "rateType")), mark(i == 0, dim(" (default)")))
			}
		}
		printUploads(c, "Documents", strList(cl, "documentIds"))
	})
}

func clientAdd(c *Ctx) error {
	cl := M{}
	fields := []struct {
		flag, label, field string
		required           bool
	}{
		{"name", "Name", "name", true},
		{"project", "Project name", "projectName", true},
		{"nature", "Nature of the work (invoice rows)", "nature", false},
		{"vat", "VAT number", "vatNumber", true},
		{"address", "Address", "address", true},
		{"city", "City (postcode + city)", "city", true},
		{"country", "Country code", "countryCode", false},
		{"email", "Email", "emailAddress", true},
		{"phone", "Phone number", "phoneNumber", false},
	}
	defs := map[string]string{"countryCode": "BE"}
	for _, f := range fields {
		var v string
		var err error
		if f.required {
			v, err = c.req(f.flag, f.label, defs[f.field])
		} else {
			v, err = c.opt(f.flag, f.label, defs[f.field])
		}
		if err != nil {
			return err
		}
		if v != "" {
			cl[f.field] = v
		}
	}
	rate, err := c.num("rate", "Rate", nil, true)
	if err != nil {
		return err
	}
	rt, err := c.enum("rate-type", "Rate per", []string{"DAYS", "HOURS"}, "DAYS")
	if err != nil {
		return err
	}
	cl["rate"], cl["rateType"] = *rate, rt
	cl["rates"] = []M{{"rate": *rate, "rateType": rt}}
	tax, err := c.num("tax-rate", "VAT rate (%)", ptr(21.0), true)
	if err != nil {
		return err
	}
	cl["taxRate"] = *tax
	days, err := c.num("days-to-pay", "Days to pay", ptr(30.0), true)
	if err != nil {
		return err
	}
	cl["maxDaysToPay"] = int(*days)
	if cl["contractStatus"], err = c.enum("status", "Contract status", contractStatuses, "ONGOING"); err != nil {
		return err
	}
	if d, err := c.date("start", "Start date", "", false); err != nil {
		return err
	} else if d != nil {
		cl["startDate"] = iso(*d)
	}
	if d, err := c.date("end", "End date", "", false); err != nil {
		return err
	} else if d != nil {
		cl["endDate"] = iso(*d)
	}
	cl["defaultWorkingDays"] = []string{"MONDAY", "TUESDAY", "WEDNESDAY", "THURSDAY", "FRIDAY"}
	if wd, ok := c.flag("working-days"); ok {
		cl["defaultWorkingDays"] = strings.Split(strings.ToUpper(wd), ",")
	}
	var saved M
	if err := c.api.Post("/api/billable-client/save", nil, cl, &saved); err != nil {
		return err
	}
	return c.result(saved, func() { info("client %s saved (%s)", str(saved, "name"), str(saved, "id")) })
}

func clientEdit(c *Ctx) error {
	cl, err := pickClient(c)
	if err != nil {
		return err
	}
	var edited M
	changed, err := editJSON(c.cfg, cl, &edited)
	if err != nil || !changed {
		if err == nil {
			info("no change")
		}
		return err
	}
	if err := c.api.Post("/api/billable-client/save", nil, edited, nil); err != nil {
		return err
	}
	info("client saved")
	return nil
}

func clientStatus(c *Ctx) error {
	cl, err := pickClient(c)
	if err != nil {
		return err
	}
	s, err := c.enum("status", "Contract status", contractStatuses, str(cl, "contractStatus"))
	if err != nil {
		return err
	}
	cl["contractStatus"] = s
	if err := c.api.Post("/api/billable-client/save", nil, cl, nil); err != nil {
		return err
	}
	info("%s: %s", str(cl, "name"), s)
	return nil
}

func clientUpload(c *Ctx) error {
	cl, err := pickClient(c)
	if err != nil {
		return err
	}
	path, err := c.file("file", "File")
	if err != nil {
		return err
	}
	if err := c.api.Multipart("/api/billable-client/upload", nil, []Part{F("id", str(cl, "id")), FFile("document", path)}, nil); err != nil {
		return err
	}
	info("%s added to %s", filepath.Base(path), str(cl, "name"))
	return nil
}

func clientDocs(c *Ctx) error {
	cl, err := pickClient(c)
	if err != nil {
		return err
	}
	ups, err := uploadsByIDs(c, strList(cl, "documentIds"))
	if err != nil {
		return err
	}
	return c.result(ups, func() { printUploads(c, "Documents of "+str(cl, "name"), strList(cl, "documentIds")) })
}

func clientDownload(c *Ctx) error {
	cl, err := pickClient(c)
	if err != nil {
		return err
	}
	if c.boolFlag("all") {
		return downloadAll(c, strList(cl, "documentIds"))
	}
	id, err := chooseUpload(c, strList(cl, "documentIds"))
	if err != nil {
		return err
	}
	return downloadAll(c, []string{id})
}

func clientRemoveDoc(c *Ctx) error {
	cl, err := pickClient(c)
	if err != nil {
		return err
	}
	id, err := chooseUpload(c, strList(cl, "documentIds"))
	if err != nil {
		return err
	}
	if err := c.confirmAction("Remove this document?"); err != nil {
		return err
	}
	if err := c.api.Delete("/api/billable-client/upload", Q("id", str(cl, "id"), "uploadId", id), nil); err != nil {
		return err
	}
	info("removed")
	return nil
}

func clientDelete(c *Ctx) error {
	cl, err := pickClient(c)
	if err != nil {
		return err
	}
	if err := c.confirmAction("Delete client " + str(cl, "name") + "?"); err != nil {
		return err
	}
	if err := c.api.Delete("/api/billable-client", Q("id", str(cl, "id")), nil); err != nil {
		return err
	}
	info("deleted")
	return nil
}

// ---------- prospects ----------

func loadProspects(c *Ctx) ([]M, error) {
	var l []M
	return l, c.api.Post("/api/form-contact/find-all", nil, nil, &l)
}

func prospectDesc(p M) string {
	return fdate(p, "creationDate") + "  " + sens(str(p, "fullName")) + "  " + truncate(str(p, "subject"), 40)
}

func prospectList(c *Ctx) error {
	l, err := loadProspects(c)
	if err != nil {
		return err
	}
	return c.result(l, func() {
		rows := [][]string{}
		for _, p := range l {
			rows = append(rows, []string{fdatetime(p, "creationDate"), sens(str(p, "fullName")), sens(str(p, "email")), sens(str(p, "phoneNumber")), str(p, "subject"), dim(str(p, "id"))})
		}
		table(c.out, []string{"received", "name", "email", "phone", "subject", "id"}, rows)
	})
}

func prospectShow(c *Ctx) error {
	m, err := c.pick("message", func() ([]M, error) { return loadProspects(c) }, prospectDesc)
	if err != nil {
		return err
	}
	if len(m) == 1 {
		l, err := loadProspects(c)
		if err != nil {
			return err
		}
		for _, p := range l {
			if str(p, "id") == str(m, "id") {
				m = p
			}
		}
	}
	return c.result(m, func() {
		details(c.out, "from", bold(sens(str(m, "fullName"))), "email", sens(str(m, "email")), "phone", sens(str(m, "phoneNumber")),
			"best time to call", str(m, "bestTimeToCall"), "received", fdatetime(m, "creationDate"), "subject", str(m, "subject"))
		fmt.Fprintln(c.out, "\n"+str(m, "body"))
	})
}

func prospectDelete(c *Ctx) error {
	ids, err := c.pickMany("messages", func() ([]M, error) { return loadProspects(c) }, prospectDesc)
	if err != nil {
		return err
	}
	if err := c.confirmAction(fmt.Sprintf("Delete %d message(s)?", len(ids))); err != nil {
		return err
	}
	for _, id := range ids {
		if err := c.api.Delete("/api/form-contact", Q("id", id), nil); err != nil {
			return err
		}
	}
	info("deleted")
	return nil
}
