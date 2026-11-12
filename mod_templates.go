package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func init() {
	register(&Module{
		Name: "templates", Aliases: []string{"template", "tpl"}, Title: "Templates", Routes: []string{"templates"}, FallbackRoles: []string{"ADMIN"},
		Help: "invoice / cv / mail... templates: edit jinja, preview with mock data",
		Actions: []*Action{
			{Name: "list", Aliases: []string{"ls"}, Help: "templates", Usage: "[--type INVOICE --version V1|V2]", Run: tplList},
			{Name: "types", Help: "template types and what they render", Run: tplTypes},
			{Name: "show", Help: "a template and the variables it reads", Usage: "<id>", Run: tplShow},
			{Name: "edit", Help: "edit a jinja (v2) template in the editor, preview, save", Usage: "<id> [--no-preview]", Run: tplEdit},
			{Name: "preview", Help: "render a template with mock (or given) data", Usage: "<id> [--file source.html] [--context ctx.json] [--edit-context] [--out path --open]", Run: tplPreview},
			{Name: "mock", Aliases: []string{"mock-context"}, Help: "the mock data a preview uses (to edit and pass with --context)", Usage: "<id> | --file source.html", Run: tplMock},
			{Name: "download", Aliases: []string{"source"}, Help: "the source of a template", Usage: "<id> [--out path | --stdout]", Run: tplDownload},
			{Name: "add", Aliases: []string{"new", "upload"}, Help: "add a template from a file", Usage: "<file> --type INVOICE [--name .. --description .. --v1 --default]", Run: tplAdd},
			{Name: "replace", Help: "replace the file of a template", Usage: "<id> <file>", Run: tplReplace},
			{Name: "default", Aliases: []string{"set-default"}, Help: "make a template the default of its type (the one rendered)", Usage: "<id>", Run: tplSetDefault},
			{Name: "delete", Aliases: []string{"rm"}, Help: "delete a template (not the default one)", Usage: "<id>", Run: tplDelete},
		},
	})
}

func loadTemplates(c *Ctx, typ string) ([]M, error) {
	var l []M
	return l, c.api.Get("/api/template/list", Q("type", typ), &l)
}

func tplDesc(t M) string {
	s := fmt.Sprintf("%s  %s  %s", str(t, "name"), dim(str(t, "type")), dim(versionLabel(str(t, "version"))))
	if boolean(t, "defaultTemplate") {
		s += green(" default")
	}
	return s
}

func versionLabel(v string) string {
	switch v {
	case "V1":
		return "v1 FreeMarker"
	case "V2":
		return "v2 Jinja"
	}
	return v
}

func tplList(c *Ctx) error {
	l, err := loadTemplates(c, strings.ToUpper(c.quiet("type", "")))
	if err != nil {
		return err
	}
	ver := strings.ToUpper(c.quiet("version", ""))
	var shown []M
	for _, t := range l {
		if ver == "" || str(t, "version") == ver {
			shown = append(shown, t)
		}
	}
	return c.result(shown, func() {
		rows := [][]string{}
		for _, t := range shown {
			rows = append(rows, []string{str(t, "name"), str(t, "type"), versionLabel(str(t, "version")), mark(boolean(t, "defaultTemplate"), green("default")),
				defStr(fdatetime(t, "updatedDate"), fdatetime(t, "dateCreation")), dim(str(t, "id"))})
		}
		table(c.out, []string{"name", "type", "version", "", "updated", "id"}, rows)
	})
}

func tplTypes(c *Ctx) error {
	var l []M
	if err := c.api.Get("/api/template/types", nil, &l); err != nil {
		return err
	}
	return c.result(l, func() {
		rows := [][]string{}
		for _, t := range l {
			rows = append(rows, []string{str(t, "type"), strings.ToLower(str(t, "output")), str(t, "extension")})
		}
		table(c.out, []string{"type", "renders", "v2 file"}, rows)
	})
}

func pickTemplate(c *Ctx) (M, error) {
	m, err := c.pick("template", func() ([]M, error) { return loadTemplates(c, strings.ToUpper(c.quiet("type", ""))) }, tplDesc)
	if err != nil {
		return nil, err
	}
	var t M
	if err := c.api.Get("/api/template/find-by-id", Q("id", str(m, "id")), &t); err != nil {
		return nil, err
	}
	return t, nil
}

func tplShow(c *Ctx) error {
	t, err := pickTemplate(c)
	if err != nil {
		return err
	}
	var vars []string
	if str(t, "version") == "V2" {
		_ = c.api.Get("/api/template/variables", Q("id", str(t, "id")), &vars)
	}
	return c.result(M{"template": t, "variables": vars}, func() {
		details(c.out, "name", bold(str(t, "name")), "description", str(t, "description"), "type", str(t, "type"),
			"version", versionLabel(str(t, "version")), "default", yesNo(boolean(t, "defaultTemplate")),
			"created", fdatetime(t, "dateCreation"), "updated", fdatetime(t, "updatedDate"), "id", str(t, "id"))
		if len(vars) > 0 {
			fmt.Fprintln(c.out, "\n"+bold("Variables"))
			for _, v := range vars {
				fmt.Fprintln(c.out, "  "+v)
			}
		}
	})
}

func templateSource(c *Ctx, t M) (string, error) {
	return c.api.FetchText(http.MethodGet, "/api/template/download", Q("id", str(t, "id")), nil, "")
}

func templateExt(c *Ctx, t M) string {
	if str(t, "version") == "V1" {
		return ".ftl"
	}
	var types []M
	_ = c.api.Get("/api/template/types", nil, &types)
	for _, x := range types {
		if str(x, "type") == str(t, "type") {
			return str(x, "extension")
		}
	}
	return ".txt"
}

func tplDownload(c *Ctx) error {
	t, err := pickTemplate(c)
	if err != nil {
		return err
	}
	src, err := templateSource(c, t)
	if err != nil {
		return err
	}
	if c.boolFlag("stdout") || !c.has("out") && !isTerminal(os.Stdout) {
		_, err := fmt.Fprint(c.out, src)
		return err
	}
	return c.save(&Download{Name: str(t, "name") + templateExt(c, t), Data: []byte(src)})
}

// mockContext: the context the backend builds from a source (or a saved template)
func mockContext(c *Ctx, id, source, ext string) (M, error) {
	parts := []Part{}
	if id != "" {
		parts = append(parts, F("id", id))
	}
	if source != "" {
		parts = append(parts, FBytes("template", "template"+ext, []byte(source)))
	}
	var m M
	if err := c.api.Multipart("/api/template/mock-context", nil, parts, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = M{}
	}
	return m, nil
}

// renderPreview renders source (or the saved template) with ctx (nil: the backend mock)
func renderPreview(c *Ctx, t M, source string, ctx M) (*Download, error) {
	parts := []Part{F("id", str(t, "id")), F("type", str(t, "type"))}
	if source != "" {
		parts = append(parts, FBytes("template", "template"+templateExt(c, t), []byte(source)), F("v1", fmt.Sprint(str(t, "version") == "V1")))
	}
	if ctx != nil {
		parts = append(parts, F("context", string(mustJSON(ctx))))
	}
	return c.api.MultipartFetch("/api/template/preview", parts, "preview")
}

// showPreview: a text rendering is printed, the rest saved (and opened)
func showPreview(c *Ctx, d *Download, name string) error {
	ext := map[string]string{"application/pdf": ".pdf", "application/xml": ".xml", "text/html": ".html", "text/calendar": ".ics"}
	ct := strings.SplitN(d.ContentType, ";", 2)[0]
	if (ct == "text/plain" || ct == "text/calendar") && !c.has("out") && !c.boolFlag("open") {
		fmt.Fprintln(c.out, dim("──── preview ────"))
		_, err := c.out.Write(d.Data)
		fmt.Fprintln(c.out, "\n"+dim("─────────────────"))
		return err
	}
	if !c.has("out") {
		p := filepath.Join(os.TempDir(), "artcoded-preview-"+sanitize(name)+defStr(ext[ct], filepath.Ext(d.Name)))
		if err := os.WriteFile(p, d.Data, 0o644); err != nil {
			return err
		}
		info("preview: %s", p)
		if !c.has("no-open") {
			if err := openInBrowser(p); err != nil {
				fmt.Fprintln(os.Stderr, dim("  (open it with your viewer)"))
			}
		}
		return nil
	}
	return c.save(d)
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ' ' || r == ':' {
			return '-'
		}
		return r
	}, s)
}

func readContextFile(c *Ctx) (M, error) {
	p, ok := c.flag("context")
	if !ok {
		return nil, nil
	}
	data, err := os.ReadFile(expandPath(p))
	if err != nil {
		return nil, err
	}
	var m M
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("--context %s: %v", p, err)
	}
	return m, nil
}

func tplPreview(c *Ctx) error {
	t, err := pickTemplate(c)
	if err != nil {
		return err
	}
	source := ""
	if f, ok := c.flag("file"); ok {
		data, err := os.ReadFile(expandPath(f))
		if err != nil {
			return err
		}
		source = string(data)
	}
	ctx, err := readContextFile(c)
	if err != nil {
		return err
	}
	if c.boolFlag("edit-context") {
		if ctx == nil {
			if ctx, err = mockContext(c, str(t, "id"), source, templateExt(c, t)); err != nil {
				return err
			}
		}
		var edited M
		if changed, err := editJSON(c.cfg, ctx, &edited); err != nil {
			return err
		} else if changed {
			ctx = edited
		}
	}
	d, err := renderPreview(c, t, source, ctx)
	if err != nil {
		return err
	}
	return showPreview(c, d, str(t, "name"))
}

func tplMock(c *Ctx) error {
	source, id, ext := "", "", ".html"
	if f, ok := c.flag("file"); ok {
		data, err := os.ReadFile(expandPath(f))
		if err != nil {
			return err
		}
		source, ext = string(data), filepath.Ext(f)
	} else {
		t, err := pickTemplate(c)
		if err != nil {
			return err
		}
		id = str(t, "id")
	}
	m, err := mockContext(c, id, source, ext)
	if err != nil {
		return err
	}
	return c.printJSON(m)
}

// tplEdit: editor → preview → save, in a loop until saved or abandoned
func tplEdit(c *Ctx) error {
	t, err := pickTemplate(c)
	if err != nil {
		return err
	}
	if str(t, "version") != "V2" {
		return errors.New("only jinja (v2) templates are edited here: download the FreeMarker file, or replace it with a jinja template")
	}
	original, err := templateSource(c, t)
	if err != nil {
		return err
	}
	ext := templateExt(c, t)
	ctx, err := readContextFile(c)
	if err != nil {
		return err
	}
	source := original
	for {
		edited, err := editText(c.cfg, source, ext)
		if err != nil {
			return err
		}
		source = edited
		if source == original {
			info("no change")
			return nil
		}
		action := "save"
		if !c.yes {
			opts := []string{"preview", "save", "edit again", "edit the preview data", "abandon the changes"}
			if c.boolFlag("no-preview") {
				opts = opts[1:]
			}
			i, err := choose("Template "+str(t, "name")+" changed", opts)
			if err != nil {
				return err
			}
			action = opts[i]
		}
		switch action {
		case "preview":
			d, err := renderPreview(c, t, source, ctx)
			if err != nil {
				fmt.Fprintln(os.Stderr, red("preview failed: ")+err.Error())
			} else if err := showPreview(c, d, str(t, "name")); err != nil {
				return err
			}
			if !askBool("Save it?", true) {
				continue
			}
			fallthrough
		case "save":
			parts := []Part{F("id", str(t, "id")), F("name", str(t, "name")), F("type", str(t, "type")), F("v1", "false"),
				FBytes("template", sanitize(str(t, "name"))+ext, []byte(source))}
			if d := str(t, "description"); d != "" {
				parts = append(parts, F("description", d))
			}
			var saved M
			if err := c.api.Multipart("/api/template/save", nil, parts, &saved); err != nil {
				fmt.Fprintln(os.Stderr, red("save failed: ")+err.Error())
				if !c.interactive || c.yes {
					return err
				}
				continue
			}
			info("template %s saved: what uses it renders with the new version", str(t, "name"))
			return nil
		case "edit the preview data":
			if ctx == nil {
				if ctx, err = mockContext(c, "", source, ext); err != nil {
					return err
				}
			}
			var edited M
			if changed, err := editJSON(c.cfg, ctx, &edited); err != nil {
				return err
			} else if changed {
				ctx = edited
			}
		case "abandon the changes":
			info("not saved")
			return nil
		}
	}
}

func tplAdd(c *Ctx) error {
	path, err := c.file("file", "Template file")
	if err != nil {
		return err
	}
	var types []M
	if err := c.api.Get("/api/template/types", nil, &types); err != nil {
		return err
	}
	names := make([]string, len(types))
	for i, t := range types {
		names[i] = str(t, "type")
	}
	typ, err := c.enum("type", "Type", names, "")
	if err != nil {
		return err
	}
	name, err := c.req("name", "Name", strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	if err != nil {
		return err
	}
	desc, err := c.opt("description", "Description", "")
	if err != nil {
		return err
	}
	v1 := c.boolFlag("v1") || strings.HasSuffix(path, ".ftl")
	def, err := c.boolOpt("default", "Make it the default "+typ+" template (the one rendered)?", false)
	if err != nil {
		return err
	}
	parts := []Part{F("name", name), F("type", typ), F("v1", fmt.Sprint(v1)), F("default", fmt.Sprint(def)), FFile("template", path)}
	if desc != "" {
		parts = append(parts, F("description", desc))
	}
	var saved M
	if err := c.api.Multipart("/api/template/save", nil, parts, &saved); err != nil {
		return err
	}
	return c.result(saved, func() { info("template %s added (%s)", name, str(saved, "id")) })
}

func tplReplace(c *Ctx) error {
	t, err := pickTemplate(c)
	if err != nil {
		return err
	}
	path, err := c.file("file", "New template file")
	if err != nil {
		return err
	}
	v1 := str(t, "version") == "V1"
	if c.has("v1") {
		v1 = c.boolFlag("v1")
	}
	parts := []Part{F("id", str(t, "id")), F("name", str(t, "name")), F("type", str(t, "type")), F("v1", fmt.Sprint(v1)), FFile("template", path)}
	if d := str(t, "description"); d != "" {
		parts = append(parts, F("description", d))
	}
	if err := c.confirmAction("Replace " + str(t, "name") + "? What uses it renders with the new file"); err != nil {
		return err
	}
	if err := c.api.Multipart("/api/template/save", nil, parts, nil); err != nil {
		return err
	}
	info("template replaced")
	return nil
}

func tplSetDefault(c *Ctx) error {
	t, err := pickTemplate(c)
	if err != nil {
		return err
	}
	if err := c.api.Post("/api/template/default", Q("id", str(t, "id")), nil, nil); err != nil {
		return err
	}
	info("%s is now the default %s template", str(t, "name"), str(t, "type"))
	return nil
}

func tplDelete(c *Ctx) error {
	t, err := pickTemplate(c)
	if err != nil {
		return err
	}
	if boolean(t, "defaultTemplate") {
		return errors.New("the default template cannot be deleted: make another one the default first")
	}
	if err := c.confirmAction("Delete template " + str(t, "name") + "?"); err != nil {
		return err
	}
	if err := c.api.Delete("/api/template", Q("id", str(t, "id")), nil); err != nil {
		return err
	}
	info("deleted")
	return nil
}
