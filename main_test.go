package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fake keycloak + backend
type fake struct {
	t           *testing.T
	mu          sync.Mutex
	calls       []string
	bodies      map[string]string
	files       map[string][]string // multipart files per path
	fields      map[string]map[string][]string
	todos       []M
	tokenGen    int
	challenge   string
	devicePolls int
}

func jwt(user string) string {
	h := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	p := base64.RawURLEncoding.EncodeToString([]byte(`{"preferred_username":"` + user + `"}`))
	return h + "." + p + ".sig"
}

func (f *fake) handler() http.Handler {
	mux := http.NewServeMux()
	writeJSON := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("/realms/R/protocol/openid-connect/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		f.calls = append(f.calls, "token:"+r.Form.Get("grant_type"))
		f.tokenGen++
		f.mu.Unlock()
		switch r.Form.Get("grant_type") {
		case "password":
			if r.Form.Get("client_id") != "cli" {
				w.WriteHeader(401)
				writeJSON(w, M{"error": "unauthorized_client", "error_description": "Client not allowed for direct access grants"})
				return
			}
			if r.Form.Get("username") != "nordine" || r.Form.Get("password") != "secret" {
				w.WriteHeader(401)
				writeJSON(w, M{"error": "invalid_grant", "error_description": "Invalid user credentials"})
				return
			}
		case "refresh_token":
		case "authorization_code":
			// PKCE: the verifier must match the challenge sent to /auth
			sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if r.Form.Get("code") != "the-code" || base64.RawURLEncoding.EncodeToString(sum[:]) != f.challenge {
				w.WriteHeader(400)
				writeJSON(w, M{"error": "invalid_grant", "error_description": "bad code or verifier"})
				return
			}
		case "urn:ietf:params:oauth:grant-type:device_code":
			f.mu.Lock()
			f.devicePolls++
			polls := f.devicePolls
			f.mu.Unlock()
			if polls < 2 {
				w.WriteHeader(400)
				writeJSON(w, M{"error": "authorization_pending"})
				return
			}
		default:
			w.WriteHeader(400)
			writeJSON(w, M{"error": "unsupported_grant_type"})
			return
		}
		writeJSON(w, M{"access_token": jwt("nordine"), "refresh_token": "rt", "expires_in": 300, "refresh_expires_in": 1800})
	})
	mux.HandleFunc("/realms/R/protocol/openid-connect/auth", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		f.mu.Lock()
		f.challenge = q.Get("code_challenge")
		f.mu.Unlock()
		if q.Get("code_challenge_method") != "S256" || q.Get("client_id") != "cli" {
			w.WriteHeader(400)
			return
		}
		http.Redirect(w, r, q.Get("redirect_uri")+"?code=the-code&state="+q.Get("state"), http.StatusFound)
	})
	mux.HandleFunc("/realms/R/protocol/openid-connect/auth/device", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, M{"device_code": "dc", "user_code": "ABCD-EFGH", "verification_uri": "http://kc/device", "expires_in": 60, "interval": 0})
	})
	mux.HandleFunc("/realms/R/protocol/openid-connect/logout", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.calls = append(f.calls, "logout")
		f.mu.Unlock()
	})
	api := func(path string, h func(w http.ResponseWriter, r *http.Request)) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
				w.WriteHeader(401)
				return
			}
			f.mu.Lock()
			f.calls = append(f.calls, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
			ct, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if ct == "multipart/form-data" {
				mr := multipart.NewReader(r.Body, params["boundary"])
				fields := map[string][]string{}
				for {
					p, err := mr.NextPart()
					if err != nil {
						break
					}
					data, _ := io.ReadAll(p)
					if p.FileName() != "" {
						f.files[r.URL.Path] = append(f.files[r.URL.Path], p.FormName()+"="+p.FileName()+":"+string(data))
					} else {
						fields[p.FormName()] = append(fields[p.FormName()], string(data))
					}
				}
				f.fields[r.URL.Path] = fields
			} else if r.Body != nil {
				data, _ := io.ReadAll(r.Body)
				f.bodies[r.URL.Path] = string(data)
			}
			f.mu.Unlock()
			h(w, r)
		})
	}
	api("/api/personal-info/@me", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, M{"username": "nordine", "email": "n@b.be", "authorities": []string{"ADMIN"}})
	})
	api("/api/settings/menu-link", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []M{
			{"title": "Home", "routerLink": []string{""}, "show": true, "order": 0},
			{"title": "Invoices", "routerLink": []string{"invoice"}, "show": true, "order": 4},
			{"title": "Gallery", "routerLink": []string{"memzagram"}, "show": false, "order": 9},
			{"title": "Expenses", "routerLink": []string{"fee"}, "show": true, "order": 3},
		})
	})
	api("/api/todo", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var t M
			_ = json.Unmarshal([]byte(f.bodies["/api/todo"]), &t)
			t["id"] = "t1"
			f.todos = append(f.todos, t)
			writeJSON(w, t)
			return
		}
		writeJSON(w, f.todos)
	})
	api("/api/invoice/page", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, M{"content": []M{{"id": "i1", "invoiceNumber": "2026-001", "billTo": M{"clientName": "ACME"}, "total": 1210.0, "locked": true, "invoiceUploadId": "u1"}},
			"page": M{"number": 0, "size": 20, "totalElements": 1, "totalPages": 1}})
	})
	api("/api/invoice/find-by-id", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, M{"id": "i1", "invoiceNumber": "2026-001", "invoiceUploadId": "u1"})
	})
	api("/api/resource/find-by-id", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, M{"id": "u1", "originalFilename": "invoice-2026-001.pdf"})
	})
	api("/api/resource/download", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="invoice-2026-001.pdf"`)
		_, _ = w.Write([]byte("%PDF-fake"))
	})
	api("/api/fee/manual-submit", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, M{"id": "f1", "subject": "x"})
	})
	api("/api/label/find-all", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []M{{"name": "TRAVEL"}})
	})
	api("/api/fee/update-tag", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, []M{}) })
	api("/api/fee/update-price", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, M{}) })
	api("/api/template/find-by-id", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, M{"id": "tp1", "name": "invoice 2027", "type": "INVOICE", "version": "V2"})
	})
	api("/api/template/types", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []M{{"type": "INVOICE", "output": "PDF", "extension": ".html"}, {"type": "SMS", "output": "TEXT", "extension": ".txt"}})
	})
	api("/api/template/preview", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", `attachment; filename="preview.pdf"`)
		_, _ = w.Write([]byte("%PDF-preview"))
	})
	api("/api/template/mock-context", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, M{"invoice": M{"total": 1234.56}})
	})
	api("/api/administrative-document/save", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(202) })
	api("/api/notification", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, []M{}) })
	return mux
}

type env struct {
	f      *fake
	srv    *httptest.Server
	dir    string
	config string
}

func newEnv(t *testing.T) *env {
	f := &fake{t: t, bodies: map[string]string{}, files: map[string][]string{}, fields: map[string]map[string][]string{}}
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	t.Setenv("HOME", dir)
	forceNonInteractive = true
	useColor = false
	return &env{f: f, srv: srv, dir: dir, config: filepath.Join(dir, ".config", "artcoded-cli.yml")}
}

// cli runs a command, returns stdout
func (e *env) cli(t *testing.T, args ...string) (string, error) {
	t.Helper()
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w
	err := run(append([]string{"--config", e.config}, args...))
	w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	return string(out), err
}

func (e *env) init(t *testing.T) {
	t.Helper()
	_, err := e.cli(t, "init", "--backend-url", e.srv.URL, "--keycloak-url", e.srv.URL, "--realm", "R", "--client-id", "cli",
		"--username", "nordine", "--password", "secret", "--login-method", "password")
	if err != nil {
		t.Fatalf("init: %v", err)
	}
}

func TestInitSavesConfigOnlyWhenLoginWorks(t *testing.T) {
	e := newEnv(t)
	_, err := e.cli(t, "init", "--backend-url", e.srv.URL, "--keycloak-url", e.srv.URL, "--realm", "R", "--client-id", "cli",
		"--username", "nordine", "--password", "wrong")
	if err == nil || !strings.Contains(err.Error(), "wrong username or password") {
		t.Fatalf("expected a credential error with a hint, got %v", err)
	}
	if _, err := os.Stat(e.config); err == nil {
		t.Fatal("config saved after a failed login")
	}
	_, err = e.cli(t, "init", "--backend-url", e.srv.URL, "--keycloak-url", e.srv.URL, "--realm", "R", "--client-id", "frontend",
		"--username", "nordine", "--password", "secret")
	if err == nil || !strings.Contains(err.Error(), "Direct access grants") {
		t.Fatalf("expected the keycloak hint, got %v", err)
	}
	e.init(t)
	st, err := os.Stat(e.config)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("config permissions %v", st.Mode().Perm())
	}
	cfg, err := loadConfig(e.config)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BackendURL != e.srv.URL || cfg.Username != "nordine" || cfg.Password != "secret" || cfg.Realm != "R" {
		t.Fatalf("config %+v", cfg)
	}
}

func TestTokenIsCachedBetweenRuns(t *testing.T) {
	e := newEnv(t)
	e.init(t)
	before := e.f.tokenGen
	out, err := e.cli(t, "whoami")
	if err != nil || !strings.Contains(out, "nordine") {
		t.Fatalf("whoami: %v %s", err, out)
	}
	if _, err := e.cli(t, "home", "todos"); err != nil {
		t.Fatal(err)
	}
	if e.f.tokenGen != before {
		t.Fatalf("token requested again (%d -> %d)", before, e.f.tokenGen)
	}
}

func TestMenuFollowsTheSettings(t *testing.T) {
	e := newEnv(t)
	e.init(t)
	cfg, _ := loadConfig(e.config)
	c := &Ctx{cfg: cfg, api: newAPI(cfg, false), out: io.Discard}
	entries, err := menuEntries(c)
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, en := range entries {
		titles = append(titles, en.title+":"+en.module.Name)
	}
	got := strings.Join(titles, ",")
	// gallery is hidden in the settings; templates and toolbox are frontend only pages for an ADMIN
	want := "Home:home,Expenses:expenses,Invoices:invoices,Templates:templates,Toolbox:toolbox"
	if got != want {
		t.Fatalf("menu\n got %s\nwant %s", got, want)
	}
}

func TestTodoAddAndList(t *testing.T) {
	e := newEnv(t)
	e.init(t)
	if _, err := e.cli(t, "home", "todo-add", "buy", "milk"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.f.bodies["/api/todo"], `"title":"buy milk"`) {
		t.Fatalf("body %s", e.f.bodies["/api/todo"])
	}
	out, err := e.cli(t, "home", "todos", "--json")
	if err != nil || !strings.Contains(out, "buy milk") {
		t.Fatalf("%v %s", err, out)
	}
}

func TestInvoiceDownload(t *testing.T) {
	e := newEnv(t)
	e.init(t)
	out := filepath.Join(e.dir, "out") + string(os.PathSeparator)
	if _, err := e.cli(t, "invoices", "download", "i1", "--out", out); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(e.dir, "out", "invoice-2026-001.pdf"))
	if err != nil || string(data) != "%PDF-fake" {
		t.Fatalf("%v %q", err, data)
	}
	if _, err := e.cli(t, "invoices", "download"); err == nil || !strings.Contains(err.Error(), "missing --id") {
		t.Fatalf("expected a missing --id error without a terminal, got %v", err)
	}
}

func TestExpenseFromFilesWithTagAndPrice(t *testing.T) {
	e := newEnv(t)
	e.init(t)
	a := filepath.Join(e.dir, "train ticket.pdf")
	b := filepath.Join(e.dir, "receipt.png")
	_ = os.WriteFile(a, []byte("A"), 0o644)
	_ = os.WriteFile(b, []byte("B"), 0o644)
	if _, err := e.cli(t, "expenses", "add", a, b, "--tag", "TRAVEL", "--price", "100,50", "--vat", "21"); err != nil {
		t.Fatal(err)
	}
	files := strings.Join(e.f.files["/api/fee/manual-submit"], "|")
	if files != "files=train ticket.pdf:A|files=receipt.png:B" {
		t.Fatalf("files %s", files)
	}
	if got := e.f.fields["/api/fee/manual-submit"]["subject"]; len(got) != 1 || got[0] != "train ticket" {
		t.Fatalf("subject defaults to the file name: %v", got)
	}
	calls := strings.Join(e.f.calls, "\n")
	if !strings.Contains(calls, "/api/fee/update-tag?tag=TRAVEL") || !strings.Contains(calls, "/api/fee/update-price?id=f1&priceHVat=100.5&vat=21") {
		t.Fatalf("calls %s", calls)
	}
}

func TestDocumentUploadDefaults(t *testing.T) {
	e := newEnv(t)
	e.init(t)
	p := filepath.Join(e.dir, "statuts.pdf")
	_ = os.WriteFile(p, []byte("S"), 0o644)
	if _, err := e.cli(t, "documents", "upload", p, "--tags", "legal, company"); err != nil {
		t.Fatal(err)
	}
	f := e.f.fields["/api/administrative-document/save"]
	if f["title"][0] != "statuts" || f["tags"][0] != "legal,company" {
		t.Fatalf("fields %v", f)
	}
}

func TestTemplatePreviewWithSourceAndContext(t *testing.T) {
	e := newEnv(t)
	e.init(t)
	src := filepath.Join(e.dir, "draft.html")
	ctx := filepath.Join(e.dir, "ctx.json")
	_ = os.WriteFile(src, []byte("<p>{{ invoice.total }}</p>"), 0o644)
	_ = os.WriteFile(ctx, []byte(`{"invoice":{"total":42}}`), 0o644)
	out := filepath.Join(e.dir, "preview.pdf")
	if _, err := e.cli(t, "templates", "preview", "tp1", "--file", src, "--context", ctx, "--out", out); err != nil {
		t.Fatal(err)
	}
	f := e.f.fields["/api/template/preview"]
	if f["type"][0] != "INVOICE" || f["v1"][0] != "false" || f["context"][0] != `{"invoice":{"total":42}}` {
		t.Fatalf("fields %v", f)
	}
	if got := strings.Join(e.f.files["/api/template/preview"], ""); got != "template=template.html:<p>{{ invoice.total }}</p>" {
		t.Fatalf("source %s", got)
	}
	if data, _ := os.ReadFile(out); string(data) != "%PDF-preview" {
		t.Fatalf("preview %q", data)
	}
	mock, err := e.cli(t, "templates", "mock", "tp1")
	if err != nil || !strings.Contains(mock, "1234.56") {
		t.Fatalf("%v %s", err, mock)
	}
}

func TestLogoutClearsTokens(t *testing.T) {
	e := newEnv(t)
	e.init(t)
	if _, err := e.cli(t, "logout"); err != nil {
		t.Fatal(err)
	}
	before := e.f.tokenGen
	if _, err := e.cli(t, "whoami"); err != nil {
		t.Fatal(err)
	}
	if e.f.tokenGen != before+1 {
		t.Fatal("expected a new login after logout (password method)")
	}
}

func TestArgs(t *testing.T) {
	a := parseArgs([]string{"x", "--json", "pos", "--tag=A", "--size", "5", "-y", "--no-open", "--no-bcc", "--", "--literal"})
	if strings.Join(a.pos, ",") != "x,pos,--literal" || a.flags["tag"][0] != "A" || a.flags["size"][0] != "5" ||
		a.flags["json"][0] != "true" || a.flags["yes"][0] != "true" || a.flags["no-open"][0] != "true" || a.flags["bcc"][0] != "false" {
		t.Fatalf("%+v", a)
	}
	if sel, err := parseRanges("1,3-4, 3", 5); err != nil || len(sel) != 3 || sel[2] != 3 {
		t.Fatalf("%v %v", sel, err)
	}
	tm, err := timesFromHours(6.4, 8)
	if err != nil || tm != [4]int{480, 720, 780, 924} {
		t.Fatalf("%v %v", tm, err)
	}
}

func TestYamlRoundTrip(t *testing.T) {
	dir := t.TempDir()
	c := &Config{path: filepath.Join(dir, "c.yml"), BackendURL: "https://api.x", Password: `p"a:ss #1`, RedirectPort: 5000}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	l, err := loadConfig(c.path)
	if err != nil || l.Password != c.Password || l.BackendURL != c.BackendURL || l.RedirectPort != 5000 {
		t.Fatalf("%v %+v", err, l)
	}
}

func TestBrowserLogin(t *testing.T) {
	e := newEnv(t)
	// the "browser" follows the redirects of keycloak back to the local callback
	browser := filepath.Join(e.dir, "browser.sh")
	_ = os.WriteFile(browser, []byte("#!/bin/sh\ncurl -sL \"$1\" > /dev/null &\n"), 0o755)
	t.Setenv("BROWSER", browser)
	_, err := e.cli(t, "init", "--backend-url", e.srv.URL, "--keycloak-url", e.srv.URL, "--realm", "R", "--client-id", "cli",
		"--login-method", "browser", "--redirect-port", fmt.Sprint(freePort(t)))
	if err != nil {
		t.Fatalf("browser login: %v", err)
	}
	if !strings.Contains(strings.Join(e.f.calls, ","), "token:authorization_code") {
		t.Fatalf("calls %v", e.f.calls)
	}
}

func TestDeviceLogin(t *testing.T) {
	e := newEnv(t)
	t.Setenv("BROWSER", "true")
	minDeviceInterval = 10 * time.Millisecond
	_, err := e.cli(t, "init", "--backend-url", e.srv.URL, "--keycloak-url", e.srv.URL, "--realm", "R", "--client-id", "cli",
		"--login-method", "device")
	if err != nil {
		t.Fatalf("device login: %v", err)
	}
	if e.f.devicePolls < 2 {
		t.Fatalf("expected polling, got %d", e.f.devicePolls)
	}
}

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
