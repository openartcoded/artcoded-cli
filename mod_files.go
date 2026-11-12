package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func init() {
	register(&Module{
		Name: "uploads", Aliases: []string{"files", "file-upload", "upload"}, Title: "Uploads", Routes: []string{"file-upload"},
		Help: "every file: upload, download, search inside files",
		Actions: []*Action{
			{Name: "list", Aliases: []string{"ls"}, Help: "files", Usage: "[--name .. --correlation-id .. --public --bookmarked --from date --to date --page n]", Run: upList},
			{Name: "show", Aliases: []string{"info"}, Help: "a file", Usage: "<id>", Run: upShow},
			{Name: "upload", Aliases: []string{"add", "put"}, Help: "upload files", Usage: "<file...> [--public --correlation-id ..]", Run: upUpload},
			{Name: "download", Aliases: []string{"get"}, Help: "download files (several: one zip with --zip)", Usage: "<id...> [--zip --out path --open]", Run: upDownload},
			{Name: "cat", Help: "print a text file", Usage: "<id>", Run: upCat},
			{Name: "search", Aliases: []string{"grep", "find"}, Help: "search inside the files (full text index)", Usage: "<query> [--mode smart|exact|prefix|fuzzy|phrase|regex|query --kinds pdf,image --page n]", Run: upSearch},
			{Name: "index-stats", Help: "state of the full text index", Run: upIndexStats},
			{Name: "bookmark", Help: "bookmark / unbookmark", Usage: "<id>", Run: func(c *Ctx) error {
				return toggleBookmark(c, "/api/resource/toggle-bookmarked", func() ([]M, error) { return searchUploads(c, M{"bookmarked": false}, 0, 100) }, upDesc)
			}},
			{Name: "links", Help: "what the files belong to (correlation labels)", Run: upLinks},
			{Name: "delete", Aliases: []string{"rm"}, Help: "delete files", Usage: "<id...>", Run: upDelete},
		},
	})
	register(&Module{
		Name: "finance", Aliases: []string{"portfolio", "stocks"}, Title: "Finance", Routes: []string{"finance"},
		Help: "portfolios and quotes",
		Actions: []*Action{
			{Name: "list", Aliases: []string{"ls"}, Help: "portfolios", Run: pfList},
			{Name: "show", Help: "a portfolio with the current prices", Usage: "<id>", Run: pfShow},
			{Name: "new", Aliases: []string{"add", "create"}, Help: "new portfolio", Usage: "<name> [--principal]", Run: pfNew},
			{Name: "rename", Help: "rename a portfolio / make it the principal one", Usage: "<id> --name .. [--principal]", Run: pfRename},
			{Name: "add-tick", Aliases: []string{"buy"}, Help: "add a symbol to a portfolio", Usage: "<portfolio id> --symbol AAPL [--price 100 --currency USD]", Run: pfAddTick},
			{Name: "remove-tick", Aliases: []string{"sell"}, Help: "remove a symbol from a portfolio", Usage: "<portfolio id> --symbol AAPL", Run: pfRemoveTick},
			{Name: "delete", Aliases: []string{"rm"}, Help: "delete a portfolio", Usage: "<id>", Run: pfDelete},
			{Name: "quote", Help: "current price of symbols", Usage: "<symbol...> [--range 5d --interval 1d]", Run: pfQuote},
			{Name: "search", Help: "find a symbol", Usage: "<query>", Run: pfSearch},
		},
	})
	register(&Module{
		Name: "sparql", Aliases: []string{"endpoint-sparql"}, Title: "Sparql", Routes: []string{"endpoint-sparql"},
		Help: "query the triplestore",
		Actions: []*Action{
			{Name: "query", Aliases: []string{"run", "select"}, Help: "run a query (argument, --file, or the editor)", Usage: "[<query>] [--file q.rq --format table|json|csv|tsv|xml|turtle]", Run: sparqlQuery},
		},
	})
}

// ---------- uploads ----------

func searchUploads(c *Ctx, cr M, page, size int) ([]M, error) {
	var p M
	if err := c.api.Post("/api/resource/find-all", Q("page", page, "size", size, "sort", "creationDate,DESC"), cr, &p); err != nil {
		return nil, err
	}
	return content(p), nil
}

func upDesc(u M) string {
	return str(u, "originalFilename") + dim("  "+humanSize(num(u, "size"))+"  "+fdate(u, "creationDate"))
}

func upList(c *Ctx) error {
	cr := M{"bookmarked": c.boolFlag("bookmarked")}
	if v, ok := c.flag("name"); ok {
		cr["originalFilename"] = v
	}
	if v, ok := c.flag("correlation-id"); ok {
		cr["correlationId"] = v
	}
	if c.has("public") {
		cr["publicResource"] = c.boolFlag("public")
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
	if err := c.api.Post("/api/resource/find-all", Q("page", page, "size", size, "sort", "creationDate,DESC"), cr, &p); err != nil {
		return err
	}
	var links map[string]any
	_ = c.api.Post("/api/resource/correlation-links", nil, nil, &links)
	return c.result(p, func() {
		rows := [][]string{}
		for _, u := range content(p) {
			belongs := ""
			if l, ok := links[str(u, "correlationId")].(string); ok {
				belongs = l
			}
			rows = append(rows, []string{str(u, "originalFilename"), humanSize(num(u, "size")), fdatetime(u, "creationDate"), belongs,
				mark(boolean(u, "publicResource"), "public") + mark(boolean(u, "bookmarked"), " ★"), dim(str(u, "id"))})
		}
		table(c.out, []string{"file", "size", "uploaded", "belongs to", "", "id"}, rows)
		fmt.Fprintln(c.out, dim(pageInfo(p)))
	})
}

func pickUpload(c *Ctx) (string, error) {
	return c.pickID("file", func() ([]M, error) { return searchUploads(c, M{"bookmarked": false}, 0, 100) }, upDesc)
}

func upShow(c *Ctx) error {
	id, err := pickUpload(c)
	if err != nil {
		return err
	}
	var u M
	if err := c.api.Get("/api/resource/find-by-id", Q("id", id), &u); err != nil {
		return err
	}
	return c.result(u, func() {
		details(c.out, "file", bold(str(u, "originalFilename")), "type", str(u, "contentType"), "size", humanSize(num(u, "size")),
			"uploaded", fdatetime(u, "creationDate"), "public", yesNo(boolean(u, "publicResource")), "correlation", str(u, "correlationId"), "id", str(u, "id"))
		if boolean(u, "publicResource") {
			fmt.Fprintln(c.out, dim("public url: "+strings.TrimRight(c.cfg.BackendURL, "/")+"/api/resource/public/download/"+id))
		}
	})
}

func upUpload(c *Ctx) error {
	files, err := c.files("file", "File", true)
	if err != nil {
		return err
	}
	public := c.boolFlag("public")
	corr := c.quiet("correlation-id", "")
	var out []M
	for _, f := range files {
		u, err := c.api.Upload(f, corr, public)
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(f), err)
		}
		out = append(out, u)
		if !c.jsonOut {
			info("%s uploaded (%s)", filepath.Base(f), str(u, "id"))
		}
	}
	if c.jsonOut {
		return c.printJSON(out)
	}
	return nil
}

func upDownload(c *Ctx) error {
	ids, err := c.pickMany("files", func() ([]M, error) { return searchUploads(c, M{"bookmarked": false}, 0, 100) }, upDesc)
	if err != nil {
		return err
	}
	if len(ids) > 1 && c.boolFlag("zip") {
		d, err := c.api.Fetch(http.MethodPost, "/api/resource/download-bulk", nil, ids, "files.zip")
		if err != nil {
			return err
		}
		return c.save(d)
	}
	return downloadAll(c, ids)
}

func upCat(c *Ctx) error {
	id, err := pickUpload(c)
	if err != nil {
		return err
	}
	d, err := c.api.Fetch(http.MethodGet, "/api/resource/download", Q("id", id), nil, id)
	if err != nil {
		return err
	}
	_, err = c.out.Write(d.Data)
	return err
}

func upLinks(c *Ctx) error {
	var links map[string]any
	if err := c.api.Post("/api/resource/correlation-links", nil, nil, &links); err != nil {
		return err
	}
	return c.result(links, func() {
		rows := [][]string{}
		for _, k := range sortedKeys(links) {
			rows = append(rows, []string{fmt.Sprint(links[k]), dim(k)})
		}
		table(c.out, []string{"label", "correlation id"}, rows)
	})
}

func upDelete(c *Ctx) error {
	ids, err := c.pickMany("files", func() ([]M, error) { return searchUploads(c, M{"bookmarked": false}, 0, 100) }, upDesc)
	if err != nil {
		return err
	}
	if err := c.confirmAction(fmt.Sprintf("Delete %d file(s)? What uses them will lose them", len(ids))); err != nil {
		return err
	}
	for _, id := range ids {
		if err := c.api.Delete("/api/resource/delete-by-id", Q("id", id), nil); err != nil {
			return err
		}
	}
	info("deleted")
	return nil
}

var tagRe = regexp.MustCompile(`<(/?)(b|em|mark|strong)>|<[^>]+>`)

// highlighted snippet of the index, for a terminal
func snippet(html string) string {
	return tagRe.ReplaceAllStringFunc(html, func(t string) string {
		switch strings.ToLower(t) {
		case "<b>", "<em>", "<mark>", "<strong>":
			if useColor {
				return "\x1b[1;33m"
			}
			return "*"
		case "</b>", "</em>", "</mark>", "</strong>":
			if useColor {
				return "\x1b[0m"
			}
			return "*"
		}
		return ""
	})
}

func upSearch(c *Ctx) error {
	q := strings.Join(c.restPos(), " ")
	if v, ok := c.flag("q"); ok {
		q = v
	}
	if q == "" {
		var err error
		if q, err = c.req("q", "Search", ""); err != nil {
			return err
		}
	}
	page, size, err := c.pageParams(20)
	if err != nil {
		return err
	}
	params := Q("q", q, "mode", c.quiet("mode", "smart"), "page", page, "per_page", size, "group_by_file", "true",
		"kinds", c.quiet("kinds", ""), "extensions", c.quiet("extensions", ""), "fields", c.quiet("fields", ""),
		"modified_from", c.quiet("from", ""), "modified_to", c.quiet("to", ""), "fuzziness", c.quiet("fuzziness", ""))
	var r M
	if err := c.api.Get("/api/file-search/v2/search", params, &r); err != nil {
		return err
	}
	return c.result(r, func() {
		hits := maps(r["hits"])
		for _, h := range hits {
			name := str(h, "upload.originalFilename")
			if name == "" {
				name = str(h, "originFileName")
			}
			where := str(h, "upload.correlationLabel")
			fmt.Fprintf(c.out, "%s %s %s\n", bold(name), dim(fmt.Sprintf("%s · %d match(es)", str(h, "kind"), int(num(h, "matches")))), dim(where))
			if s := strings.TrimSpace(snippet(str(h, "snippetHtml"))); s != "" {
				fmt.Fprintf(c.out, "  %s\n", strings.ReplaceAll(truncate(s, 400), "\n", " "))
			}
			if id := str(h, "upload.id"); id != "" {
				fmt.Fprintf(c.out, "  %s\n", dim("id "+id))
			}
		}
		more := ""
		if boolean(r, "hasMore") {
			more = fmt.Sprintf(" · more: --page %d", page+2)
		}
		fmt.Fprintln(c.out, dim(fmt.Sprintf("%d hit(s), %d match(es) in %sms%s", len(hits), int(num(r, "totalMatches")), str(r, "tookMs"), more)))
	})
}

func upIndexStats(c *Ctx) error {
	var s M
	if err := c.api.Get("/api/file-search/stats", nil, &s); err != nil {
		return err
	}
	if c.jsonOut {
		return c.printJSON(s)
	}
	details(c.out, "discovered", str(s, "discovered"), "indexed", str(s, "indexed"), "pending", str(s, "pending"), "skipped", str(s, "skipped"), "duplicate", str(s, "duplicate"))
	if k, ok := s["kinds"].(map[string]any); ok {
		for _, kind := range sortedKeys(k) {
			fmt.Fprintf(c.out, "  %s %v\n", dim(kind), k[kind])
		}
	}
	return nil
}

// ---------- finance ----------

func loadPortfolios(c *Ctx) ([]M, error) {
	var l []M
	return l, c.api.Post("/api/finance/portfolio/find-all", nil, nil, &l)
}

func pfDesc(p M) string {
	return str(p, "name") + dim(fmt.Sprintf("  %d symbol(s)%s", len(list(p, "ticks")), mark(boolean(p, "principal"), " principal")))
}

func pfList(c *Ctx) error {
	l, err := loadPortfolios(c)
	if err != nil {
		return err
	}
	return c.result(l, func() {
		rows := [][]string{}
		for _, p := range l {
			rows = append(rows, []string{str(p, "name"), fmt.Sprint(len(list(p, "ticks"))), mark(boolean(p, "principal"), "★"), fdate(p, "updatedDate"), dim(str(p, "id"))})
		}
		table(c.out, []string{"portfolio", "symbols", "principal", "updated", "id"}, rows)
	})
}

func pickPortfolio(c *Ctx) (M, error) {
	m, err := c.pick("portfolio", func() ([]M, error) { return loadPortfolios(c) }, pfDesc)
	if err != nil || len(m) > 1 {
		return m, err
	}
	var p M
	if err := c.api.Post("/api/finance/portfolio/find-by-id", Q("id", str(m, "id")), nil, &p); err != nil {
		return nil, err
	}
	return p, nil
}

type quote struct {
	price, previous float64
	currency, name  string
}

func fetchQuote(c *Ctx, symbol, rng, interval string) (*quote, M, error) {
	var r M
	if err := c.api.Get("/proxy/yahoo-finance/chart/"+url.PathEscape(symbol), Q("interval", interval, "range", rng, "region", "EU"), &r); err != nil {
		return nil, nil, err
	}
	res := maps(get(r, "chart.result"))
	if len(res) == 0 {
		return nil, r, fmt.Errorf("%s: no quote", symbol)
	}
	meta := get(res[0], "meta")
	q := &quote{price: num(meta, "regularMarketPrice"), previous: num(meta, "chartPreviousClose"), currency: str(meta, "currency"), name: defStr(str(meta, "longName"), str(meta, "shortName"))}
	return q, r, nil
}

func pct(a, b float64) string {
	if b == 0 {
		return ""
	}
	v := (a - b) / b * 100
	s := fmt.Sprintf("%+.2f%%", v)
	if v < 0 {
		return red(s)
	}
	return green(s)
}

func pfShow(c *Ctx) error {
	p, err := pickPortfolio(c)
	if err != nil {
		return err
	}
	if c.jsonOut {
		return c.printJSON(p)
	}
	details(c.out, "portfolio", bold(str(p, "name")), "principal", yesNo(boolean(p, "principal")), "id", str(p, "id"))
	rows := [][]string{}
	for _, t := range maps(p["ticks"]) {
		row := []string{str(t, "symbol"), fmt.Sprintf("%.2f %s", num(t, "priceWhenAdded"), str(t, "currency")), fdate(t, "addedDate"), "", "", ""}
		if q, _, err := fetchQuote(c, str(t, "symbol"), "5d", "1d"); err == nil {
			row[3] = fmt.Sprintf("%.2f %s", q.price, q.currency)
			row[4] = pct(q.price, q.previous)
			row[5] = pct(q.price, num(t, "priceWhenAdded"))
		}
		rows = append(rows, row)
	}
	fmt.Fprintln(c.out)
	table(c.out, []string{"symbol", "added at", "added", "now", "5 days", "since added"}, rows)
	return nil
}

func pfNew(c *Ctx) error {
	name, err := c.reqPos("name", "Name", "")
	if err != nil {
		return err
	}
	var saved M
	if err := c.api.Post("/api/finance/portfolio/save", nil, M{"name": name, "principal": c.boolFlag("principal"), "ticks": []any{}}, &saved); err != nil {
		return err
	}
	return c.result(saved, func() { info("portfolio %s created (%s)", name, str(saved, "id")) })
}

func pfRename(c *Ctx) error {
	p, err := pickPortfolio(c)
	if err != nil {
		return err
	}
	name, err := c.req("name", "Name", str(p, "name"))
	if err != nil {
		return err
	}
	principal, err := c.boolOpt("principal", "Principal portfolio?", boolean(p, "principal"))
	if err != nil {
		return err
	}
	p["name"], p["principal"] = name, principal
	if err := c.api.Post("/api/finance/portfolio/save", nil, p, nil); err != nil {
		return err
	}
	info("saved")
	return nil
}

func pfAddTick(c *Ctx) error {
	p, err := pickPortfolio(c)
	if err != nil {
		return err
	}
	symbol, err := c.req("symbol", "Symbol (e.g. AAPL, IWDA.AS)", "")
	if err != nil {
		return err
	}
	symbol = strings.ToUpper(symbol)
	q, _, qerr := fetchQuote(c, symbol, "1d", "1d")
	var def *float64
	cur := "EUR"
	if qerr == nil {
		def, cur = &q.price, defStr(q.currency, "EUR")
		if q.name != "" {
			info("%s: %s, %.2f %s", symbol, q.name, q.price, cur)
		}
	} else {
		warn("no quote for %s: %v", symbol, qerr)
	}
	price, err := c.num("price", "Price when added", def, true)
	if err != nil {
		return err
	}
	currency := c.quiet("currency", cur)
	tick := M{"symbol": symbol, "priceWhenAdded": *price, "currency": currency, "addedDate": iso(time.Now())}
	ticks := append(maps(p["ticks"]), tick)
	if err := c.api.Post("/api/finance/portfolio/update-ticks", Q("id", str(p, "id")), ticks, nil); err != nil {
		return err
	}
	info("%s added to %s", symbol, str(p, "name"))
	return nil
}

func pfRemoveTick(c *Ctx) error {
	p, err := pickPortfolio(c)
	if err != nil {
		return err
	}
	symbol, ok := c.flag("symbol")
	if !ok {
		ticks := maps(p["ticks"])
		if len(ticks) == 0 {
			return errors.New("empty portfolio")
		}
		if !c.interactive {
			return missing("symbol")
		}
		opts := make([]string, len(ticks))
		for i, t := range ticks {
			opts[i] = str(t, "symbol")
		}
		i, err := choose("Symbol", opts)
		if err != nil {
			return err
		}
		symbol = opts[i]
	}
	if err := c.api.Delete("/api/finance/portfolio/tick", Q("id", str(p, "id"), "symbol", symbol), nil); err != nil {
		return err
	}
	info("%s removed", symbol)
	return nil
}

func pfDelete(c *Ctx) error {
	p, err := pickPortfolio(c)
	if err != nil {
		return err
	}
	if err := c.confirmAction("Delete portfolio " + str(p, "name") + "?"); err != nil {
		return err
	}
	if err := c.api.Delete("/api/finance/portfolio", Q("id", str(p, "id")), nil); err != nil {
		return err
	}
	info("deleted")
	return nil
}

func pfQuote(c *Ctx) error {
	symbols := c.restPos()
	if len(symbols) == 0 {
		s, err := c.req("symbol", "Symbol", "")
		if err != nil {
			return err
		}
		symbols = []string{s}
	}
	rng, interval := c.quiet("range", "5d"), c.quiet("interval", "1d")
	var raw []M
	rows := [][]string{}
	for _, s := range symbols {
		q, r, err := fetchQuote(c, strings.ToUpper(s), rng, interval)
		raw = append(raw, r)
		if err != nil {
			rows = append(rows, []string{strings.ToUpper(s), red(err.Error()), "", ""})
			continue
		}
		rows = append(rows, []string{strings.ToUpper(s), q.name, fmt.Sprintf("%.2f %s", q.price, q.currency), pct(q.price, q.previous)})
	}
	return c.result(raw, func() { table(c.out, []string{"symbol", "name", "price", rng}, rows) })
}

func pfSearch(c *Ctx) error {
	q := strings.Join(c.restPos(), " ")
	if q == "" {
		var err error
		if q, err = c.req("q", "Search", ""); err != nil {
			return err
		}
	}
	var r M
	if err := c.api.Get("/proxy/yahoo-finance/search", Q("q", q), &r); err != nil {
		return err
	}
	return c.result(r, func() {
		rows := [][]string{}
		for _, x := range maps(r["quotes"]) {
			rows = append(rows, []string{str(x, "symbol"), defStr(str(x, "longname"), str(x, "shortname")), str(x, "exchDisp"), str(x, "typeDisp")})
		}
		table(c.out, []string{"symbol", "name", "exchange", "type"}, rows)
	})
}

// ---------- sparql ----------

func sparqlQuery(c *Ctx) error {
	endpoint := c.cfg.SparqlEndpoint
	if endpoint == "" {
		endpoint = strings.TrimRight(c.cfg.BackendURL, "/") + "/sparql"
		warn("sparql_endpoint not set, using %s (artcoded-cli config set sparql_endpoint <url>)", endpoint)
	}
	query := strings.Join(c.restPos(), " ")
	if f, ok := c.flag("file"); ok {
		data, err := os.ReadFile(expandPath(f))
		if err != nil {
			return err
		}
		query = string(data)
	}
	if query == "" {
		var err error
		query, err = c.longText("query", "Query", "SELECT * WHERE {\n  ?s ?p ?o\n} LIMIT 20\n", true)
		if err != nil {
			return err
		}
	}
	format := strings.ToLower(c.quiet("format", "table"))
	if c.jsonOut {
		format = "json"
	}
	accept := map[string]string{
		"table": "application/sparql-results+json", "json": "application/sparql-results+json", "csv": "text/csv",
		"tsv": "text/tab-separated-values", "xml": "application/sparql-results+xml", "turtle": "text/turtle", "ntriples": "application/n-triples",
	}[format]
	if accept == "" {
		return fmt.Errorf("unknown format %q", format)
	}
	isUpdate := regexp.MustCompile(`(?i)^\s*(PREFIX[^\n]*\n\s*)*(INSERT|DELETE|LOAD|CLEAR|CREATE|DROP|COPY|MOVE|ADD|WITH)\b`).MatchString(query)
	form := url.Values{"query": {query}}
	if isUpdate {
		form = url.Values{"update": {query}}
	}
	if strings.Contains(strings.ToUpper(query), "CONSTRUCT") || strings.Contains(strings.ToUpper(query), "DESCRIBE") {
		if format == "table" || format == "json" {
			accept = "text/turtle"
			format = "turtle"
		}
	}
	res, err := c.api.send(request{method: http.MethodPost, path: endpoint, absolute: true, body: []byte(form.Encode()),
		contentType: "application/x-www-form-urlencoded", accept: accept + ", */*;q=0.1"})
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if format != "table" {
		_, err := c.out.Write(data)
		return err
	}
	var r M
	if err := json.Unmarshal(data, &r); err != nil {
		_, err := c.out.Write(data)
		return err
	}
	if b, ok := r["boolean"].(bool); ok {
		fmt.Fprintln(c.out, b)
		return nil
	}
	vars := strList(r, "head.vars")
	rows := [][]string{}
	for _, b := range maps(get(r, "results.bindings")) {
		row := make([]string, len(vars))
		for i, v := range vars {
			row[i] = str(b, v+".value")
		}
		rows = append(rows, row)
	}
	table(c.out, vars, rows)
	fmt.Fprintln(c.out, dim(fmt.Sprintf("%d row(s)", len(rows))))
	return nil
}
