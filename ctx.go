package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // Europe/Brussels everywhere
)

// boolean flags: never take the next argument as their value
var boolFlags = map[string]bool{
	"json": true, "yes": true, "help": true, "verbose": true, "open": true, "no-open": true,
	"archived": true, "deleted": true, "bookmarked": true, "closed": true, "default": true, "v1": true,
	"public": true, "payment-proof": true, "bcc": true, "visible": true, "hidden": true, "refresh": true,
	"physical": true, "all": true, "done": true, "raw": true, "force": true, "snapshot": true, "edit": true,
	"stdout": true, "quiet": true, "no-color": true, "unseen": true, "processed": true, "student": true,
	"group": true, "mock": true, "insecure": true, "proof": true, "all-days": true,
	"from-previous": true, "principal": true, "zip": true, "xml": true, "edit-context": true, "mail": true, "sms": true,
	"in-app": true, "persist": true, "demo": true, "online": true, "version": true, "save": true, "no-preview": true, "enable": true, "disable": true,
}

var shortFlags = map[string]string{"V": "version", "y": "yes", "h": "help", "j": "json", "v": "verbose", "o": "out", "q": "quiet"}

type Args struct {
	flags map[string][]string
	pos   []string
}

func parseArgs(argv []string) Args {
	a := Args{flags: map[string][]string{}}
	for i := 0; i < len(argv); i++ {
		t := argv[i]
		switch {
		case t == "--":
			a.pos = append(a.pos, argv[i+1:]...)
			return a
		case strings.HasPrefix(t, "--") && len(t) > 2:
			name, val, hasVal := strings.Cut(t[2:], "=")
			if !hasVal {
				if boolFlags[name] || strings.HasPrefix(name, "no-") && boolFlags[name[3:]] {
					val = "true"
					if strings.HasPrefix(name, "no-") && !boolFlags[name] {
						name, val = name[3:], "false"
					}
				} else if i+1 < len(argv) {
					i++
					val = argv[i]
				} else {
					val = "true"
				}
			}
			a.flags[name] = append(a.flags[name], val)
		case len(t) == 2 && t[0] == '-' && shortFlags[t[1:]] != "":
			name := shortFlags[t[1:]]
			val := "true"
			if !boolFlags[name] && i+1 < len(argv) {
				i++
				val = argv[i]
			}
			a.flags[name] = append(a.flags[name], val)
		default:
			a.pos = append(a.pos, t)
		}
	}
	return a
}

// Ctx of a command: arguments, api, output.
type Ctx struct {
	api  *API
	cfg  *Config
	args Args
	out  io.Writer
	used map[string]bool
	// interactive: questions may be asked
	interactive bool
	jsonOut     bool
	yes         bool
}

func (c *Ctx) has(name string) bool { _, ok := c.args.flags[name]; return ok }

func (c *Ctx) flag(name string) (string, bool) {
	v, ok := c.args.flags[name]
	if !ok || len(v) == 0 {
		return "", false
	}
	return v[len(v)-1], true
}

func (c *Ctx) flags(name string) []string { return c.args.flags[name] }

// pos: next positional argument
func (c *Ctx) pos() (string, bool) {
	if len(c.args.pos) == 0 {
		return "", false
	}
	v := c.args.pos[0]
	c.args.pos = c.args.pos[1:]
	return v, true
}

func (c *Ctx) restPos() []string {
	v := c.args.pos
	c.args.pos = nil
	return v
}

// flagOrPos: --name, or the next positional argument
func (c *Ctx) flagOrPos(name string) (string, bool) {
	if v, ok := c.flag(name); ok {
		return v, true
	}
	return c.pos()
}

func missing(name string) error {
	return fmt.Errorf("missing --%s (run in a terminal to be asked)", name)
}

// req: required value from --name, else asked.
func (c *Ctx) req(name, label, def string) (string, error) {
	if v, ok := c.flag(name); ok && v != "" {
		return v, nil
	}
	if !c.interactive {
		if def != "" {
			return def, nil
		}
		return "", missing(name)
	}
	return askRequired(label, def)
}

// reqPos: like req, but the first positional argument works too.
func (c *Ctx) reqPos(name, label, def string) (string, error) {
	if v, ok := c.flagOrPos(name); ok && v != "" {
		return v, nil
	}
	return c.req(name, label, def)
}

// opt: optional value from --name, else asked (enter keeps def) when interactive.
func (c *Ctx) opt(name, label, def string) (string, error) {
	if v, ok := c.flag(name); ok {
		return v, nil
	}
	if !c.interactive || c.yes {
		return def, nil
	}
	return ask(label, def)
}

// quiet: value from --name only, never asked.
func (c *Ctx) quiet(name, def string) string {
	if v, ok := c.flag(name); ok {
		return v
	}
	return def
}

func parseBool(v string) (bool, error) {
	switch strings.ToLower(v) {
	case "true", "yes", "y", "1", "on":
		return true, nil
	case "false", "no", "n", "0", "off", "":
		return false, nil
	}
	return false, fmt.Errorf("not a boolean: %q", v)
}

// boolOpt: --name / --no-name, else asked when interactive
func (c *Ctx) boolOpt(name, label string, def bool) (bool, error) {
	if v, ok := c.flag(name); ok {
		return parseBool(v)
	}
	if !c.interactive || c.yes {
		return def, nil
	}
	return confirm(label, def)
}

func (c *Ctx) boolFlag(name string) bool {
	v, ok := c.flag(name)
	if !ok {
		return false
	}
	b, _ := parseBool(v)
	return b
}

func (c *Ctx) intFlag(name string, def int) (int, error) {
	v, ok := c.flag(name)
	if !ok || v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("--%s: not a number: %s", name, v)
	}
	return n, nil
}

func parseNumber(v string) (float64, error) {
	v = strings.TrimSpace(strings.ReplaceAll(strings.TrimSuffix(strings.TrimSpace(v), "€"), ",", "."))
	return strconv.ParseFloat(v, 64)
}

// num: number from --name or asked (def used when empty)
func (c *Ctx) num(name, label string, def *float64, required bool) (*float64, error) {
	ds := ""
	if def != nil {
		ds = strconv.FormatFloat(*def, 'f', -1, 64)
	}
	for {
		var v string
		var err error
		if required {
			v, err = c.req(name, label, ds)
		} else {
			v, err = c.opt(name, label, ds)
		}
		if err != nil {
			return nil, err
		}
		if v == "" {
			return nil, nil
		}
		f, perr := parseNumber(v)
		if perr == nil {
			return &f, nil
		}
		if c.has(name) || !c.interactive {
			return nil, fmt.Errorf("--%s: not a number: %s", name, v)
		}
		fmt.Fprintln(os.Stderr, red("  not a number"))
	}
}

// enum: one of values, from --name or chosen.
func (c *Ctx) enum(name, label string, values []string, def string) (string, error) {
	if v, ok := c.flag(name); ok {
		for _, x := range values {
			if strings.EqualFold(x, v) {
				return x, nil
			}
		}
		return "", fmt.Errorf("--%s must be one of %s", name, strings.Join(values, ", "))
	}
	if !c.interactive || c.yes {
		if def != "" {
			return def, nil
		}
		return "", missing(name)
	}
	opts := make([]string, len(values))
	for i, v := range values {
		opts[i] = v
		if v == def {
			opts[i] += dim(" (current)")
		}
	}
	i, err := choose(label, opts)
	if err != nil {
		return "", err
	}
	return values[i], nil
}

// date from --name or asked: YYYY-MM-DD, today, +3d...
func (c *Ctx) date(name, label, def string, required bool) (*time.Time, error) {
	for {
		var v string
		var err error
		if required {
			v, err = c.req(name, label+" (YYYY-MM-DD)", def)
		} else {
			v, err = c.opt(name, label+" (YYYY-MM-DD, empty for none)", def)
		}
		if err != nil {
			return nil, err
		}
		if v == "" {
			return nil, nil
		}
		t, perr := parseDate(v)
		if perr == nil {
			return &t, nil
		}
		if c.has(name) || !c.interactive {
			return nil, fmt.Errorf("--%s: %v", name, perr)
		}
		fmt.Fprintln(os.Stderr, red("  "+perr.Error()))
	}
}

var brussels = func() *time.Location {
	l, err := time.LoadLocation("Europe/Brussels")
	if err != nil {
		return time.Local
	}
	return l
}()

func parseDate(v string) (time.Time, error) {
	v = strings.TrimSpace(strings.ToLower(v))
	now := time.Now().In(brussels)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, brussels)
	switch v {
	case "today", "now":
		return today, nil
	case "yesterday":
		return today.AddDate(0, 0, -1), nil
	case "tomorrow":
		return today.AddDate(0, 0, 1), nil
	}
	if (strings.HasPrefix(v, "+") || strings.HasPrefix(v, "-")) && len(v) > 2 {
		n, err := strconv.Atoi(v[1 : len(v)-1])
		if err == nil {
			if v[0] == '-' {
				n = -n
			}
			switch v[len(v)-1] {
			case 'd':
				return today.AddDate(0, 0, n), nil
			case 'w':
				return today.AddDate(0, 0, 7*n), nil
			case 'm':
				return today.AddDate(0, n, 0), nil
			}
		}
	}
	for _, f := range []string{"2006-01-02", "02/01/2006", "2006-01-02 15:04", "2006-01-02T15:04", time.RFC3339, "02/01/2006 15:04"} {
		if t, err := time.ParseInLocation(f, v, brussels); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("not a date: %q (YYYY-MM-DD, DD/MM/YYYY, today, +3d...)", v)
}

// iso: json date for the backend
func iso(t time.Time) string { return t.Format("2006-01-02T15:04:05.000Z07:00") }

// httpDate: java.util.Date request parameter format (like Date.toUTCString in the backoffice)
func httpDate(t time.Time) string { return t.UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT") }

// existing file path from --name, positional or asked
func (c *Ctx) file(name, label string) (string, error) {
	for {
		v, err := c.reqPos(name, label, "")
		if err != nil {
			return "", err
		}
		p := expandPath(v)
		st, serr := os.Stat(p)
		if serr == nil && !st.IsDir() {
			return p, nil
		}
		msg := "no such file: " + p
		if serr == nil {
			msg = p + " is a directory"
		}
		if c.has(name) || !c.interactive {
			return "", errors.New(msg)
		}
		fmt.Fprintln(os.Stderr, red("  "+msg))
	}
}

// files from --name (repeatable), positionals, or asked (one per line, empty to stop)
func (c *Ctx) files(name, label string, required bool) ([]string, error) {
	raw := append(append([]string{}, c.flags(name)...), c.restPos()...)
	var out []string
	for _, r := range raw {
		matches, _ := filepath.Glob(expandPath(r))
		if len(matches) == 0 {
			matches = []string{expandPath(r)}
		}
		for _, p := range matches {
			st, err := os.Stat(p)
			if err != nil || st.IsDir() {
				return nil, fmt.Errorf("no such file: %s", p)
			}
			out = append(out, p)
		}
	}
	if len(out) > 0 || !c.interactive || c.yes && !required {
		if required && len(out) == 0 {
			return nil, missing(name)
		}
		return out, nil
	}
	fmt.Fprintln(os.Stderr, dim("  one path per line (drag & drop works), empty line to finish"))
	for {
		v, err := ask(fmt.Sprintf("%s #%d", label, len(out)+1), "")
		if err != nil {
			return nil, err
		}
		if v == "" {
			if required && len(out) == 0 {
				fmt.Fprintln(os.Stderr, red("  at least one file"))
				continue
			}
			return out, nil
		}
		p := expandPath(v)
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			fmt.Fprintln(os.Stderr, red("  no such file: "+p))
			continue
		}
		out = append(out, p)
	}
}

// longText: --name, --name-file, else the editor (or a single line when asked).
func (c *Ctx) longText(name, label, def string, required bool) (string, error) {
	if v, ok := c.flag(name); ok {
		return v, nil
	}
	if p, ok := c.flag(name + "-file"); ok {
		if p == "-" {
			data, err := io.ReadAll(os.Stdin)
			return string(data), err
		}
		data, err := os.ReadFile(expandPath(p))
		return string(data), err
	}
	if !c.interactive || c.yes {
		if required && def == "" {
			return "", missing(name)
		}
		return def, nil
	}
	useEditor, err := confirm(label+": open the editor?", def != "" && strings.Contains(def, "\n") || def == "")
	if err != nil {
		return "", err
	}
	if useEditor {
		v, err := editText(c.cfg, def, ".md")
		if err != nil {
			return "", err
		}
		v = strings.TrimRight(v, "\n")
		if required && strings.TrimSpace(v) == "" {
			return "", errors.New(label + " is required")
		}
		return v, nil
	}
	if required {
		return askRequired(label, def)
	}
	return ask(label, def)
}

func (c *Ctx) confirmAction(label string) error {
	if c.yes {
		return nil
	}
	if !c.interactive {
		return errors.New(label + ": add --yes to confirm")
	}
	ok, err := confirm(label, false)
	if err != nil {
		return err
	}
	if !ok {
		return errCancel
	}
	return nil
}

// printJSON: the raw value, for --json
func (c *Ctx) printJSON(v any) error {
	enc := json.NewEncoder(c.out)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// result: prints json with --json, otherwise calls human()
func (c *Ctx) result(v any, human func()) error {
	if c.jsonOut {
		return c.printJSON(v)
	}
	human()
	return nil
}

// save a download: --out (file or directory), download_dir, or the current directory. --stdout writes it out.
func (c *Ctx) save(d *Download) error {
	if c.boolFlag("stdout") {
		_, err := c.out.Write(d.Data)
		return err
	}
	dir := "."
	if c.cfg.DownloadDir != "" {
		dir = expandPath(c.cfg.DownloadDir)
	}
	name := d.Name
	if name == "" {
		name = "download"
	}
	target := filepath.Join(dir, name)
	if o, ok := c.flag("out"); ok {
		o = expandPath(o)
		if st, err := os.Stat(o); err == nil && st.IsDir() || strings.HasSuffix(o, string(os.PathSeparator)) {
			target = filepath.Join(o, name)
		} else {
			target = o
		}
	} else {
		target = uniquePath(target)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(target, d.Data, 0o644); err != nil {
		return err
	}
	info("saved %s (%s)", target, humanSize(float64(len(d.Data))))
	if c.boolFlag("open") {
		if err := openInBrowser(target); err != nil {
			warn("cannot open %s: %v", target, err)
		}
	}
	return nil
}

func uniquePath(p string) string {
	if _, err := os.Stat(p); err != nil {
		return p
	}
	ext := filepath.Ext(p)
	base := strings.TrimSuffix(p, ext)
	for i := 1; ; i++ {
		c := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if _, err := os.Stat(c); err != nil {
			return c
		}
	}
}

func humanSize(b float64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	i := 0
	for b >= 1024 && i < len(units)-1 {
		b /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f %s", b, units[i])
	}
	return fmt.Sprintf("%.1f %s", b, units[i])
}

// ---------- json helpers (entities are kept as maps: unknown fields survive an edit) ----------

type M = map[string]any

func get(m any, path string) any {
	cur := m
	for _, p := range strings.Split(path, ".") {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[p]
	}
	return cur
}

func str(m any, path string) string {
	switch v := get(m, path).(type) {
	case nil:
		return ""
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	case []any:
		parts := make([]string, 0, len(v))
		for _, x := range v {
			parts = append(parts, fmt.Sprint(x))
		}
		return strings.Join(parts, ", ")
	default:
		return fmt.Sprint(v)
	}
}

func num(m any, path string) float64 {
	switch v := get(m, path).(type) {
	case float64:
		return v
	case string:
		f, _ := strconv.ParseFloat(v, 64)
		return f
	}
	return 0
}

func boolean(m any, path string) bool { b, _ := get(m, path).(bool); return b }

func list(m any, path string) []any { l, _ := get(m, path).([]any); return l }

func strList(m any, path string) []string {
	var out []string
	for _, x := range list(m, path) {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func maps(v any) []M {
	l, _ := v.([]any)
	out := make([]M, 0, len(l))
	for _, x := range l {
		if m, ok := x.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func toTime(v any) (time.Time, bool) {
	switch x := v.(type) {
	case string:
		for _, f := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.000-0700", "2006-01-02T15:04:05.000+0000", "2006-01-02T15:04:05", "2006-01-02"} {
			if t, err := time.Parse(f, x); err == nil {
				return t.In(brussels), true
			}
		}
	case float64:
		return time.UnixMilli(int64(x)).In(brussels), true
	}
	return time.Time{}, false
}

func fdate(m any, path string) string {
	if t, ok := toTime(get(m, path)); ok {
		return t.Format("2006-01-02")
	}
	return str(m, path)
}

func fdatetime(m any, path string) string {
	if t, ok := toTime(get(m, path)); ok {
		return t.Format("2006-01-02 15:04")
	}
	return str(m, path)
}

// demoMode: amounts and names of people / companies are masked (--demo, demo: true in the config)
var demoMode bool

// sens masks a sensitive text (a client, an email, a phone) in demo mode
func sens(s string) string {
	if !demoMode || strings.TrimSpace(s) == "" {
		return s
	}
	r := []rune(strings.TrimSpace(s))
	return string(r[0]) + "•••••"
}

func money(v float64) string {
	if demoMode {
		return "•••• €"
	}
	neg := v < 0
	v = math.Abs(v)
	s := strconv.FormatFloat(v, 'f', 2, 64)
	intPart, dec, _ := strings.Cut(s, ".")
	var b strings.Builder
	for i, r := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteRune(' ')
		}
		b.WriteRune(r)
	}
	out := b.String() + "," + dec + " €"
	if neg {
		return "-" + out
	}
	return out
}

func moneyOf(m any, path string) string {
	if get(m, path) == nil {
		return ""
	}
	return money(num(m, path))
}

func yesNo(b bool) string {
	if b {
		return green("yes")
	}
	return "no"
}

func mark(b bool, s string) string {
	if b {
		return s
	}
	return ""
}

func pageInfo(p M) string {
	if pg, ok := p["page"].(map[string]any); ok {
		return fmt.Sprintf("page %d/%d · %d total", int(num(pg, "number"))+1, max(int(num(pg, "totalPages")), 1), int(num(pg, "totalElements")))
	}
	// spring Page without VIA_DTO serialization
	return fmt.Sprintf("page %d/%d · %d total", int(num(p, "number"))+1, max(int(num(p, "totalPages")), 1), int(num(p, "totalElements")))
}

func (c *Ctx) pageParams(defSize int) (int, int, error) {
	page, err := c.intFlag("page", 1)
	if err != nil {
		return 0, 0, err
	}
	size, err := c.intFlag("size", defSize)
	if err != nil {
		return 0, 0, err
	}
	if page < 1 {
		page = 1
	}
	return page - 1, size, nil
}

// dedupe: items with the same id once
func dedupe(items []M) []M {
	seen := map[string]bool{}
	out := items[:0:0]
	for _, it := range items {
		id := str(it, "id")
		if id != "" && seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, it)
	}
	return out
}

// pick an entity: --id, positional, or chosen from the list
func (c *Ctx) pick(label string, load func() ([]M, error), describe func(M) string) (M, error) {
	if id, ok := c.flagOrPos("id"); ok && id != "" {
		return M{"id": id}, nil
	}
	if !c.interactive {
		return nil, missing("id")
	}
	items, err := load()
	if err != nil {
		return nil, err
	}
	items = dedupe(items)
	if len(items) == 0 {
		return nil, fmt.Errorf("no %s", label)
	}
	opts := make([]string, len(items))
	for i, it := range items {
		opts[i] = describe(it)
	}
	i, err := choose("Choose "+label, opts)
	if err != nil {
		return nil, err
	}
	return items[i], nil
}

func (c *Ctx) pickID(label string, load func() ([]M, error), describe func(M) string) (string, error) {
	m, err := c.pick(label, load, describe)
	if err != nil {
		return "", err
	}
	return str(m, "id"), nil
}

// pickMany: ids from --id (repeatable) / positionals, or chosen
func (c *Ctx) pickMany(label string, load func() ([]M, error), describe func(M) string) ([]string, error) {
	ids := append(append([]string{}, c.flags("id")...), c.restPos()...)
	if len(ids) > 0 {
		return ids, nil
	}
	if !c.interactive {
		return nil, missing("id")
	}
	items, err := load()
	if err != nil {
		return nil, err
	}
	items = dedupe(items)
	if len(items) == 0 {
		return nil, fmt.Errorf("no %s", label)
	}
	opts := make([]string, len(items))
	for i, it := range items {
		opts[i] = describe(it)
	}
	sel, err := chooseMany("Choose "+label, opts)
	if err != nil {
		return nil, err
	}
	for _, i := range sel {
		ids = append(ids, str(items[i], "id"))
	}
	return ids, nil
}

func content(p M) []M { return maps(p["content"]) }

func timeSinceDays(t time.Time) float64 { return time.Since(t).Hours() / 24 }
