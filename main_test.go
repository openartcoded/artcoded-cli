package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
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
	menu        []M
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
		if f.menu != nil {
			writeJSON(w, f.menu)
			return
		}
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
	api("/api/personal-info", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, M{"demoMode": false}) })
	api("/api/timesheet/estimate-total-to-be-invoiced-this-month", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 7200.0) })
	api("/api/dossier/active-dossier", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, M{"id": "d1", "name": "2026-Q4", "tvaToBePaid": 1234.5})
	})
	api("/api/fee/search", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, M{"content": []M{{"id": "f9", "subject": "Train Brussels-Paris"}}, "page": M{"totalElements": 4, "totalPages": 1, "number": 0}})
	})
	api("/api/timesheet", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, M{"2026": M{"ACME": []M{{"id": "ts1", "name": "November", "clientName": "ACME", "year": 2026, "month": 11, "numberOfHoursWorked": 56, "numberOfWorkingDays": 7}}}})
	})
	api("/api/invoice/find-all-summaries", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []M{
			{"period": "01/2025", "client": "ACME", "amount": 20, "amountType": "DAYS", "subTotal": 12000, "dateOfInvoice": "2025-02-01T00:00:00.000+00:00"},
			{"period": "02/2025", "client": "Globex", "amount": 80, "amountType": "HOURS", "hoursPerDay": 8, "subTotal": 6000, "dateOfInvoice": "2025-03-01T00:00:00.000+00:00"},
			{"period": "01/2026", "client": "ACME", "amount": 10, "amountType": "DAYS", "subTotal": 7000, "dateOfInvoice": "2026-02-01T00:00:00.000+00:00"},
		})
	})
	api("/api/timesheet/stats", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, M{"years": []int{2025, 2026}, "clients": []string{"ACME", "Globex"},
			"rows":       []M{{"year": 2025, "month": 1, "client": "ACME", "minutes": 9600, "days": 20}, {"year": 2026, "month": 1, "client": "ACME", "minutes": 4800, "days": 10}},
			"workedDays": M{"2025": 25, "2026": 10}})
	})
	return mux
}

type env struct {
	f      *fake
	srv    *httptest.Server
	dir    string
	config string
}

func newEnv(t *testing.T) *env { return newEnvTLS(t, false) }

func newEnvTLS(t *testing.T, tlsServer bool) *env {
	f := &fake{t: t, bodies: map[string]string{}, files: map[string][]string{}, fields: map[string]map[string][]string{}}
	srv := httptest.NewUnstartedServer(f.handler())
	if tlsServer {
		srv.StartTLS()
	} else {
		srv.Start()
	}
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	// no test opens a real browser (the ones checking what is opened set their own)
	t.Setenv("BROWSER", "true")
	t.Setenv("HOME", dir)
	forceNonInteractive = true
	useColor = false
	return &env{f: f, srv: srv, dir: dir, config: filepath.Join(dir, ".config", "artcoded-cli.yml")}
}

// cli runs a command, returns stdout
func (e *env) cli(t *testing.T, args ...string) (string, error) {
	t.Helper()
	r, w, _ := os.Pipe()
	old, oldW := os.Stdout, stdout
	os.Stdout, stdout = w, w
	err := run(append([]string{"--config", e.config}, args...))
	w.Close()
	os.Stdout, stdout = old, oldW
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
	// gallery is hidden in the settings: not shown. Statistics follow home; templates is a page of the backoffice
	// not in the backend menu (shown to an ADMIN); the toolbox follows administration, absent here
	want := "Home:home,Statistics:stats,Expenses:expenses,Invoices:invoices,Templates:templates"
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

func TestSelfSignedCertificate(t *testing.T) {
	e := newEnvTLS(t, true)
	base := []string{"init", "--backend-url", e.srv.URL, "--keycloak-url", e.srv.URL, "--realm", "R", "--client-id", "cli",
		"--username", "nordine", "--password", "secret"}
	_, err := e.cli(t, base...)
	if err == nil || !strings.Contains(err.Error(), "ca_file") {
		t.Fatalf("expected a certificate error with the hint, got %v", err)
	}
	// the certificate of the server as ca_file
	ca := filepath.Join(e.dir, "ca.pem")
	_ = os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: e.srv.Certificate().Raw}), 0o644)
	if _, err := e.cli(t, append(base, "--ca-file", ca)...); err != nil {
		t.Fatalf("with ca_file: %v", err)
	}
	if out, err := e.cli(t, "whoami"); err != nil || !strings.Contains(out, "nordine") {
		t.Fatalf("whoami with ca_file: %v", err)
	}
	cfg, _ := loadConfig(e.config)
	if cfg.CAFile != ca {
		t.Fatalf("ca_file not saved: %+v", cfg)
	}
	// or not checked at all
	_ = os.Remove(e.config)
	if _, err := e.cli(t, append(base, "--insecure")...); err != nil {
		t.Fatalf("insecure: %v", err)
	}
	if cfg, _ := loadConfig(e.config); !cfg.Insecure {
		t.Fatal("insecure not saved")
	}
	// a broken ca file is reported
	_ = os.WriteFile(ca, []byte("nope"), 0o644)
	if _, err := e.cli(t, append(base, "--ca-file", ca, "--insecure=false")...); err == nil || !strings.Contains(err.Error(), "no PEM") {
		t.Fatalf("expected a pem error, got %v", err)
	}
}

func TestMenuHidesWhatTheSettingsHide(t *testing.T) {
	e := newEnv(t)
	e.init(t)
	e.f.menu = []M{
		{"title": "Home", "routerLink": []string{""}, "show": false, "order": 0},
		{"title": "Templates", "routerLink": []string{"templates"}, "show": false, "order": 1},
		{"title": "Admin", "routerLink": []string{"administration"}, "show": true, "order": 2},
	}
	cfg, _ := loadConfig(e.config)
	entries, err := menuEntries(&Ctx{cfg: cfg, api: newAPI(cfg, false), out: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, en := range entries {
		got = append(got, en.title+":"+en.module.Name)
	}
	// home hidden: no statistics either; templates hidden in the settings: not added back
	if strings.Join(got, ",") != "Admin:admin,Toolbox:toolbox" {
		t.Fatalf("menu %v", got)
	}
}

func TestResetForgetsEverything(t *testing.T) {
	e := newEnv(t)
	e.init(t)
	cfg, _ := loadConfig(e.config)
	cache := tokenCachePath(cfg)
	if _, err := os.Stat(cache); err != nil {
		t.Fatal("tokens cached after init")
	}
	if _, err := e.cli(t, "reset"); err == nil {
		t.Fatal("reset without a terminal needs --yes")
	}
	if _, err := e.cli(t, "reset", "--yes"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{e.config, cache} {
		if _, err := os.Stat(p); err == nil {
			t.Fatalf("%s still there", p)
		}
	}
	if !strings.Contains(strings.Join(e.f.calls, ","), "logout") {
		t.Fatal("not logged out of keycloak")
	}
	e.init(t)
}

func TestStats(t *testing.T) {
	e := newEnv(t)
	e.init(t)
	out, err := e.cli(t, "stats", "years")
	if err != nil {
		t.Fatal(err)
	}
	// 2025: 18000 for 20 + 10 days invoiced (80 hours / 8): 600 per day; relative: 18000 / 25 days worked = 720
	for _, want := range []string{"18 000,00 €", "30", "600,00 €", "720,00 €", "700,00 €", "all time: 25 000,00 €"} {
		if !strings.Contains(out, want) {
			t.Errorf("stats years misses %q:\n%s", want, out)
		}
	}
	out, _ = e.cli(t, "stats", "clients")
	if !strings.Contains(out, "ACME") || !strings.Contains(out, " 76%") {
		t.Errorf("stats clients:\n%s", out)
	}
	out, _ = e.cli(t, "stats", "months", "--year", "2025")
	if !strings.Contains(out, "02/2025") || strings.Contains(out, "01/2026") {
		t.Errorf("stats months:\n%s", out)
	}
	out, _ = e.cli(t, "stats", "hours-clients")
	if !strings.Contains(out, "160") || !strings.Contains(out, "Total") {
		t.Errorf("stats hours-clients:\n%s", out)
	}
	if bar(1, 1, 4) != "████" || bar(1, 8*4, 4) != "▏" || bar(0, 1, 4) != "" {
		t.Error("bars")
	}
}

func TestDashboard(t *testing.T) {
	e := newEnv(t)
	e.init(t)
	out, err := e.cli(t, "dashboard")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"To invoice this month", "7 200,00 €", "Unprocessed expenses", "4", "Train Brussels-Paris",
		"2026-Q4", "Revenue per month", "Cumulated revenue", "Open timesheets", "ACME", "56 h"} {
		if !strings.Contains(out, want) {
			t.Errorf("dashboard misses %q:\n%s", want, out)
		}
	}
}

func TestDemoMasks(t *testing.T) {
	e := newEnv(t)
	e.init(t)
	out, err := e.cli(t, "dashboard", "--demo")
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"7 200", "ACME", "1 234"} {
		if strings.Contains(out, leak) {
			t.Errorf("demo shows %q:\n%s", leak, out)
		}
	}
	if !strings.Contains(out, "•••• €") || !strings.Contains(out, "A•••••") {
		t.Errorf("demo masks:\n%s", out)
	}
	out, _ = e.cli(t, "stats", "clients", "--demo")
	if strings.Contains(out, "ACME") || strings.Contains(out, "19 000") {
		t.Errorf("stats clients in demo:\n%s", out)
	}
	if _, err := e.cli(t, "invoices", "list", "--demo", "--json"); err == nil {
		t.Error("--json in demo mode must be refused")
	}
	demoMode = false
}

func TestOnline(t *testing.T) {
	e := newEnv(t)
	e.init(t)
	opened := filepath.Join(e.dir, "opened")
	browser := filepath.Join(e.dir, "browser.sh")
	_ = os.WriteFile(browser, []byte("#!/bin/sh\necho \"$1\" >> "+opened+"\n"), 0o755)
	t.Setenv("BROWSER", browser)
	if _, err := e.cli(t, "expenses", "list", "--online"); err == nil || !strings.Contains(err.Error(), "frontend_url") {
		t.Fatalf("expected the frontend_url hint, got %v", err)
	}
	if _, err := e.cli(t, "config", "set", "frontend_url", "https://bo.example.org"); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"expenses", "list", "--processed"}, {"timesheets", "show", "ts9"}, {"dashboard"}, {"invoices", "list", "--deleted"}} {
		if _, err := e.cli(t, append(args, "--online")...); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	if _, err := e.cli(t, "expenses", "tag", "--online"); err == nil {
		t.Error("an action without a page refuses --online")
	}
	// the browsers are started in the background: wait for the 4 of them, in any order
	var lines []string
	for i := 0; i < 100 && len(lines) < 4; i++ {
		time.Sleep(20 * time.Millisecond)
		got, _ := os.ReadFile(opened)
		lines = strings.Fields(string(got))
	}
	sort.Strings(lines)
	want := []string{"https://bo.example.org/", "https://bo.example.org/fee/processed",
		"https://bo.example.org/invoice/deleted-logically", "https://bo.example.org/timesheets/timesheet/ts9"}
	if strings.Join(lines, " ") != strings.Join(want, " ") {
		t.Fatalf("opened %v, want %v", lines, want)
	}
}

func TestUsageFollowsTheMenu(t *testing.T) {
	e := newEnv(t)
	e.init(t)
	cfg, _ := loadConfig(e.config)
	// gallery hidden in the settings
	if _, err := menuEntries(&Ctx{cfg: cfg, api: newAPI(cfg, false), out: io.Discard}); err != nil {
		t.Fatal(err)
	}
	out, _ := e.cli(t)
	if strings.Contains(out, "gallery") || !strings.Contains(out, "invoices") || !strings.Contains(out, "stats") {
		t.Fatalf("usage:\n%s", out)
	}
	if strings.Contains(out, "tasks (") {
		t.Fatalf("tasks is not in the menu of the fake backend:\n%s", out)
	}
	if v, _ := e.cli(t, "--version"); !strings.HasPrefix(v, "artcoded ") {
		t.Fatalf("version %q", v)
	}
}
