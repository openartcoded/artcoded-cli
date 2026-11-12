package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Tokens issued by keycloak, cached on disk between runs.
type Tokens struct {
	AccessToken      string    `json:"access_token"`
	RefreshToken     string    `json:"refresh_token"`
	IDToken          string    `json:"id_token,omitempty"`
	ExpiresAt        time.Time `json:"expires_at"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
}

type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	IDToken          string `json:"id_token"`
	ExpiresIn        int    `json:"expires_in"`
	RefreshExpiresIn int    `json:"refresh_expires_in"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

type oauthError struct {
	Code, Description string
	Status            int
}

func (e *oauthError) Error() string {
	msg := e.Code
	if e.Description != "" {
		msg += ": " + e.Description
	}
	if hint := oauthHint(e); hint != "" {
		msg += "\n  hint: " + hint
	}
	return "keycloak: " + msg
}

// what to change in keycloak, for the usual errors
func oauthHint(e *oauthError) string {
	d := strings.ToLower(e.Description)
	switch {
	case e.Code == "unauthorized_client" && strings.Contains(d, "direct access"):
		return "enable 'Direct access grants' on the client (Clients > <client> > Settings > Capability config), or use login_method: browser"
	case e.Code == "unauthorized_client" && strings.Contains(d, "device"):
		return "enable 'OAuth 2.0 Device Authorization Grant' on the client, or use another login_method"
	case e.Code == "unauthorized_client":
		return "the client may be confidential: set client_secret, or use a public client"
	case e.Code == "invalid_client":
		return "unknown client_id, or a confidential client without client_secret"
	case e.Code == "invalid_grant" && strings.Contains(d, "invalid user credentials"):
		return "wrong username or password"
	case e.Code == "invalid_grant" && strings.Contains(d, "not fully set up"):
		return "the account has required actions (OTP, update password...): log in once in the browser, or use login_method: browser"
	case e.Code == "invalid_redirect_uri" || strings.Contains(d, "redirect"):
		return "add http://127.0.0.1:<redirect_port>/* to 'Valid redirect URIs' of the client"
	}
	return ""
}

func (t *tokenResponse) toTokens() *Tokens {
	now := time.Now()
	tk := &Tokens{AccessToken: t.AccessToken, RefreshToken: t.RefreshToken, IDToken: t.IDToken,
		ExpiresAt: now.Add(time.Duration(t.ExpiresIn) * time.Second)}
	if t.RefreshExpiresIn > 0 {
		tk.RefreshExpiresAt = now.Add(time.Duration(t.RefreshExpiresIn) * time.Second)
	} else if t.RefreshToken != "" {
		// 0 = offline token, no expiry
		tk.RefreshExpiresAt = now.Add(365 * 24 * time.Hour)
	}
	return tk
}

func (t *Tokens) Valid() bool {
	return t != nil && t.AccessToken != "" && time.Until(t.ExpiresAt) > 20*time.Second
}

func (t *Tokens) CanRefresh() bool {
	return t != nil && t.RefreshToken != "" && time.Until(t.RefreshExpiresAt) > 10*time.Second
}

// Claims of the access token (not verified: the backend does it)
func (t *Tokens) Claims() map[string]any {
	parts := strings.Split(t.AccessToken, ".")
	if len(parts) < 2 {
		return nil
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	return m
}

type Auth struct {
	cfg  *Config
	http *http.Client
}

func tokenCachePath(cfg *Config) string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	h := sha256.Sum256([]byte(cfg.BackendURL + "|" + cfg.realmURL() + "|" + cfg.ClientID + "|" + cfg.Username))
	return filepath.Join(dir, "artcoded-cli", fmt.Sprintf("tokens-%x.json", h[:6]))
}

func (a *Auth) loadCached() *Tokens {
	data, err := os.ReadFile(tokenCachePath(a.cfg))
	if err != nil {
		return nil
	}
	var t Tokens
	if json.Unmarshal(data, &t) != nil {
		return nil
	}
	return &t
}

func (a *Auth) saveCached(t *Tokens) {
	p := tokenCachePath(a.cfg)
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	data, _ := json.Marshal(t)
	_ = os.WriteFile(p, data, 0o600)
}

func (a *Auth) clearCached() { _ = os.Remove(tokenCachePath(a.cfg)) }

func (a *Auth) tokenRequest(form url.Values) (*Tokens, error) {
	form.Set("client_id", a.cfg.ClientID)
	if a.cfg.ClientSecret != "" {
		form.Set("client_secret", a.cfg.ClientSecret)
	}
	res, err := a.http.PostForm(a.cfg.realmURL()+"/protocol/openid-connect/token", form)
	if err != nil {
		return nil, fmt.Errorf("cannot reach keycloak (%s): %w", a.cfg.KeycloakURL, err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		if res.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("keycloak realm not found at %s (check keycloak_url and realm; older keycloak needs the /auth suffix)", a.cfg.realmURL())
		}
		return nil, fmt.Errorf("keycloak: unexpected answer (%s): %s", res.Status, truncate(string(body), 200))
	}
	if tr.Error != "" || res.StatusCode >= 400 {
		return nil, &oauthError{Code: tr.Error, Description: tr.ErrorDescription, Status: res.StatusCode}
	}
	return tr.toTokens(), nil
}

func (a *Auth) PasswordLogin(username, password string) (*Tokens, error) {
	if username == "" || password == "" {
		return nil, errors.New("username and password are required for login_method: password")
	}
	return a.tokenRequest(url.Values{"grant_type": {"password"}, "username": {username}, "password": {password}, "scope": {"openid"}})
}

func (a *Auth) Refresh(t *Tokens) (*Tokens, error) {
	return a.tokenRequest(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {t.RefreshToken}})
}

func randomString(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// BrowserLogin: authorization code + PKCE, the browser comes back on a local http server.
func (a *Auth) BrowserLogin() (*Tokens, error) {
	port := a.cfg.Port()
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, fmt.Errorf("cannot listen on 127.0.0.1:%d for the login callback (set redirect_port): %w", port, err)
	}
	redirect := fmt.Sprintf("http://127.0.0.1:%d/callback", port)
	verifier := randomString(32)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	state := randomString(16)
	q := url.Values{
		"client_id": {a.cfg.ClientID}, "response_type": {"code"}, "scope": {"openid"},
		"redirect_uri": {redirect}, "state": {state},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"},
	}
	if a.cfg.Username != "" {
		q.Set("login_hint", a.cfg.Username)
	}
	authURL := a.cfg.realmURL() + "/protocol/openid-connect/auth?" + q.Encode()

	type result struct {
		code string
		err  error
	}
	done := make(chan result, 1)
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second}
	srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback" {
			http.NotFound(w, r)
			return
		}
		p := r.URL.Query()
		var res result
		switch {
		case p.Get("error") != "":
			res.err = &oauthError{Code: p.Get("error"), Description: p.Get("error_description")}
		case p.Get("state") != state:
			res.err = errors.New("login callback with an unexpected state, try again")
		default:
			res.code = p.Get("code")
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		msg := "You are logged in, you can close this tab and go back to the terminal."
		if res.err != nil {
			msg = "Login failed: " + html.EscapeString(res.err.Error())
		}
		fmt.Fprintf(w, `<!doctype html><html><body style="font-family:sans-serif;padding:3em"><h2>artcoded</h2><p>%s</p></body></html>`, msg)
		select {
		case done <- res:
		default:
		}
	})
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	fmt.Fprintln(stderr, "Opening the browser to log in. If it doesn't open, visit:")
	fmt.Fprintln(stderr, "  "+authURL)
	fmt.Fprintln(stderr, dim(fmt.Sprintf("If keycloak answers 'Invalid parameter: redirect_uri': add http://127.0.0.1:%d/* to the\n"+
		"'Valid redirect URIs' of the client %s, then try again (ctrl+c to stop waiting).", port, a.cfg.ClientID)))
	_ = openInBrowser(authURL)

	select {
	case res := <-done:
		if res.err != nil {
			return nil, res.err
		}
		return a.tokenRequest(url.Values{"grant_type": {"authorization_code"}, "code": {res.code},
			"redirect_uri": {redirect}, "code_verifier": {verifier}})
	case <-time.After(5 * time.Minute):
		return nil, errors.New("login timed out (5 minutes)")
	}
}

// poll interval of the device login when keycloak asks less (a var for the tests)
var minDeviceInterval = 5 * time.Second

// DeviceLogin: OAuth 2.0 device authorization grant (no local port needed, works over ssh).
func (a *Auth) DeviceLogin() (*Tokens, error) {
	form := url.Values{"client_id": {a.cfg.ClientID}, "scope": {"openid"}}
	if a.cfg.ClientSecret != "" {
		form.Set("client_secret", a.cfg.ClientSecret)
	}
	res, err := a.http.PostForm(a.cfg.realmURL()+"/protocol/openid-connect/auth/device", form)
	if err != nil {
		return nil, err
	}
	var dr struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int    `json:"expires_in"`
		Interval                int    `json:"interval"`
		Error                   string `json:"error"`
		ErrorDescription        string `json:"error_description"`
	}
	err = json.NewDecoder(res.Body).Decode(&dr)
	res.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("keycloak device endpoint: %s", res.Status)
	}
	if dr.Error != "" {
		return nil, &oauthError{Code: dr.Error, Description: dr.ErrorDescription, Status: res.StatusCode}
	}
	link := dr.VerificationURIComplete
	if link == "" {
		link = dr.VerificationURI
	}
	fmt.Fprintf(stderr, "Open %s and confirm the code %s\n", link, bold(dr.UserCode))
	_ = openInBrowser(link)
	interval := max(time.Duration(dr.Interval)*time.Second, minDeviceInterval)
	deadline := time.Now().Add(time.Duration(max(dr.ExpiresIn, 60)) * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(interval)
		t, err := a.tokenRequest(url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "device_code": {dr.DeviceCode}})
		var oe *oauthError
		if errors.As(err, &oe) {
			switch oe.Code {
			case "authorization_pending":
				continue
			case "slow_down":
				interval += 5 * time.Second
				continue
			}
		}
		return t, err
	}
	return nil, errors.New("device login expired")
}

// Login with the configured method (interactive for browser / device).
func (a *Auth) Login() (*Tokens, error) {
	var t *Tokens
	var err error
	switch a.cfg.Method() {
	case "browser":
		t, err = a.BrowserLogin()
	case "device":
		t, err = a.DeviceLogin()
	case "password":
		pw, perr := a.cfg.ResolvePassword()
		if perr != nil {
			return nil, perr
		}
		if pw == "" && isInteractive() {
			pw, perr = askSecret("Password for " + a.cfg.Username)
			if perr != nil {
				return nil, perr
			}
		}
		t, err = a.PasswordLogin(a.cfg.Username, pw)
	default:
		return nil, fmt.Errorf("unknown login_method %q (password, browser or device)", a.cfg.LoginMethod)
	}
	if err != nil {
		return nil, err
	}
	a.saveCached(t)
	return t, nil
}

// Token: a valid access token, refreshed or obtained again when needed.
func (a *Auth) Token(force bool) (*Tokens, error) {
	t := a.loadCached()
	if !force && t.Valid() {
		return t, nil
	}
	if t.CanRefresh() {
		if nt, err := a.Refresh(t); err == nil {
			a.saveCached(nt)
			return nt, nil
		}
	}
	if a.cfg.Method() != "password" && !isInteractive() {
		return nil, errors.New("session expired: run 'artcoded login' in a terminal")
	}
	return a.Login()
}

func (a *Auth) Logout() error {
	t := a.loadCached()
	a.clearCached()
	if t == nil || t.RefreshToken == "" {
		return nil
	}
	form := url.Values{"client_id": {a.cfg.ClientID}, "refresh_token": {t.RefreshToken}}
	if a.cfg.ClientSecret != "" {
		form.Set("client_secret", a.cfg.ClientSecret)
	}
	res, err := a.http.PostForm(a.cfg.realmURL()+"/protocol/openid-connect/logout", form)
	if err != nil {
		return err
	}
	res.Body.Close()
	return nil
}
