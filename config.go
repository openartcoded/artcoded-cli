package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Config is stored as a flat yaml file (key: value), ~/.config/artcoded-cli.yml by default.
type Config struct {
	BackendURL     string
	KeycloakURL    string
	Realm          string
	ClientID       string
	ClientSecret   string
	Username       string
	Password       string
	PasswordCmd    string
	LoginMethod    string // password | browser | device
	RedirectPort   int
	SparqlEndpoint string
	FrontendURL    string
	Editor         string
	DownloadDir    string
	CAFile         string
	Insecure       bool

	path string
}

var configKeys = []struct {
	key     string
	comment string
	get     func(c *Config) string
	set     func(c *Config, v string)
}{
	{"backend_url", "url of the artcoded backend api", func(c *Config) string { return c.BackendURL }, func(c *Config, v string) { c.BackendURL = v }},
	{"frontend_url", "url of the backoffice (used to discover the keycloak settings)", func(c *Config) string { return c.FrontendURL }, func(c *Config, v string) { c.FrontendURL = v }},
	{"keycloak_url", "keycloak base url, as KEYCLOAK_URL of the backoffice", func(c *Config) string { return c.KeycloakURL }, func(c *Config, v string) { c.KeycloakURL = v }},
	{"realm", "keycloak realm", func(c *Config) string { return c.Realm }, func(c *Config, v string) { c.Realm = v }},
	{"client_id", "keycloak client (public client, see README)", func(c *Config) string { return c.ClientID }, func(c *Config, v string) { c.ClientID = v }},
	{"client_secret", "only for a confidential client", func(c *Config) string { return c.ClientSecret }, func(c *Config, v string) { c.ClientSecret = v }},
	{"login_method", "password | browser | device", func(c *Config) string { return c.LoginMethod }, func(c *Config, v string) { c.LoginMethod = v }},
	{"username", "", func(c *Config) string { return c.Username }, func(c *Config, v string) { c.Username = v }},
	{"password", "plain text: prefer password_command (e.g. pass show artcoded)", func(c *Config) string { return c.Password }, func(c *Config, v string) { c.Password = v }},
	{"password_command", "command printing the password", func(c *Config) string { return c.PasswordCmd }, func(c *Config, v string) { c.PasswordCmd = v }},
	{"redirect_port", "local port of the browser login callback", func(c *Config) string {
		if c.RedirectPort == 0 {
			return ""
		}
		return strconv.Itoa(c.RedirectPort)
	}, func(c *Config, v string) { c.RedirectPort, _ = strconv.Atoi(v) }},
	{"sparql_endpoint", "", func(c *Config) string { return c.SparqlEndpoint }, func(c *Config, v string) { c.SparqlEndpoint = v }},
	{"editor", "editor for long texts / json (default: $VISUAL, $EDITOR, vi)", func(c *Config) string { return c.Editor }, func(c *Config, v string) { c.Editor = v }},
	{"download_dir", "where downloads go (default: current directory)", func(c *Config) string { return c.DownloadDir }, func(c *Config, v string) { c.DownloadDir = v }},
	{"ca_file", "pem file of the certificate (authority) of a self-signed backend / keycloak", func(c *Config) string { return c.CAFile }, func(c *Config, v string) { c.CAFile = v }},
	{"insecure", "true: certificates are not checked (local development only)", func(c *Config) string {
		if c.Insecure {
			return "true"
		}
		return ""
	}, func(c *Config, v string) { c.Insecure, _ = parseBool(v) }},
}

const defaultRedirectPort = 53682

func defaultConfigPath() string {
	if p := os.Getenv("ARTCODED_CONFIG"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "artcoded-cli.yml"
	}
	return filepath.Join(home, ".config", "artcoded-cli.yml")
}

func loadConfig(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	c := &Config{path: path}
	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		raw := strings.TrimSpace(sc.Text())
		if raw == "" || strings.HasPrefix(raw, "#") || raw == "---" {
			continue
		}
		k, v, ok := strings.Cut(raw, ":")
		if !ok {
			return nil, fmt.Errorf("%s:%d: expected 'key: value'", path, line)
		}
		k = strings.TrimSpace(k)
		val, err := yamlScalar(strings.TrimSpace(v))
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %v", path, line, err)
		}
		for _, ck := range configKeys {
			if ck.key == k {
				ck.set(c, val)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	c.applyEnv()
	return c, nil
}

// environment variables win over the file (handy in scripts / CI)
func (c *Config) applyEnv() {
	for _, ck := range configKeys {
		if v := os.Getenv("ARTCODED_" + strings.ToUpper(ck.key)); v != "" {
			ck.set(c, v)
		}
	}
}

func yamlScalar(v string) (string, error) {
	switch {
	case v == "" || v == "~" || v == "null":
		return "", nil
	case strings.HasPrefix(v, `"`):
		end := strings.LastIndex(v, `"`)
		if end <= 0 {
			return "", fmt.Errorf("unterminated string")
		}
		return strconv.Unquote(v[:end+1])
	case strings.HasPrefix(v, "'"):
		end := strings.LastIndex(v, "'")
		if end <= 0 {
			return "", fmt.Errorf("unterminated string")
		}
		return strings.ReplaceAll(v[1:end], "''", "'"), nil
	default:
		if i := strings.Index(v, " #"); i >= 0 {
			v = strings.TrimSpace(v[:i])
		}
		return v, nil
	}
}

func (c *Config) Save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# artcoded configuration (artcoded config edit)\n")
	for _, ck := range configKeys {
		v := ck.get(c)
		if v == "" {
			continue
		}
		if ck.comment != "" {
			b.WriteString("# " + ck.comment + "\n")
		}
		b.WriteString(ck.key + ": " + strconv.Quote(v) + "\n")
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

func (c *Config) Map() map[string]string {
	m := map[string]string{}
	for _, ck := range configKeys {
		m[ck.key] = ck.get(c)
	}
	return m
}

func (c *Config) Set(key, value string) error {
	for _, ck := range configKeys {
		if ck.key == key {
			ck.set(c, value)
			return nil
		}
	}
	keys := make([]string, 0, len(configKeys))
	for _, ck := range configKeys {
		keys = append(keys, ck.key)
	}
	sort.Strings(keys)
	return fmt.Errorf("unknown key %q (one of %s)", key, strings.Join(keys, ", "))
}

func (c *Config) Method() string {
	if c.LoginMethod == "" {
		return "password"
	}
	return c.LoginMethod
}

func (c *Config) Port() int {
	if c.RedirectPort == 0 {
		return defaultRedirectPort
	}
	return c.RedirectPort
}

// ResolvePassword: the password, from password_command when set.
func (c *Config) ResolvePassword() (string, error) {
	if c.Password != "" {
		return c.Password, nil
	}
	if c.PasswordCmd == "" {
		return "", nil
	}
	shell, flag := "sh", "-c"
	if isWindows() {
		shell, flag = "cmd", "/C"
	}
	out, err := exec.Command(shell, flag, c.PasswordCmd).Output()
	if err != nil {
		return "", fmt.Errorf("password_command: %w", err)
	}
	return strings.SplitN(strings.TrimRight(string(out), "\r\n"), "\n", 2)[0], nil
}

func (c *Config) realmURL() string {
	return strings.TrimRight(c.KeycloakURL, "/") + "/realms/" + c.Realm
}

func (c *Config) Validate() error {
	var missing []string
	if c.BackendURL == "" {
		missing = append(missing, "backend_url")
	}
	if c.KeycloakURL == "" {
		missing = append(missing, "keycloak_url")
	}
	if c.Realm == "" {
		missing = append(missing, "realm")
	}
	if c.ClientID == "" {
		missing = append(missing, "client_id")
	}
	if len(missing) > 0 {
		return fmt.Errorf("config %s: missing %s (run: artcoded init)", c.path, strings.Join(missing, ", "))
	}
	if _, err := c.tlsConfig(); err != nil {
		return err
	}
	return nil
}
