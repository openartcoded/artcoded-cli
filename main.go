package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

var version = "dev"

type Action struct {
	Name    string
	Aliases []string
	Usage   string // arguments, e.g. "<id> [--out path]"
	Help    string
	Run     func(c *Ctx) error
}

type Module struct {
	Name    string
	Aliases []string
	Title   string
	Help    string
	// router links of the backoffice menu showing this module ("" is home)
	Routes []string
	// roles allowed when the menu of the backend doesn't know the route (frontend only pages)
	FallbackRoles []string
	Actions       []*Action
}

var modules []*Module

func register(m *Module) { modules = append(modules, m) }

func findModule(name string) *Module {
	name = strings.ToLower(name)
	for _, m := range modules {
		if m.Name == name {
			return m
		}
		for _, a := range m.Aliases {
			if a == name {
				return m
			}
		}
	}
	return nil
}

func (m *Module) find(name string) *Action {
	name = strings.ToLower(name)
	for _, a := range m.Actions {
		if a.Name == name {
			return a
		}
		for _, al := range a.Aliases {
			if al == name {
				return a
			}
		}
	}
	return nil
}

const usageHeader = `artcoded-cli: the artcoded backoffice in your terminal

Usage:
  artcoded-cli                       interactive menu (the one of the backoffice settings)
  artcoded-cli <module> <action> [arguments] [--flags]
  artcoded-cli <module>              choose an action
  artcoded-cli help <module>         actions of a module

Missing arguments are asked in a terminal. Use --yes to skip questions/confirmations
(scripts), --json for the raw json, --out <file|dir> and --open for downloads.

Setup:
  init                   create / change the configuration and log in
  login | logout | whoami
  config [show|path|edit|set <key> <value>]
  completion bash|zsh    shell completion: source <(artcoded-cli completion bash)

Global flags:
  --config <path>        configuration file (default ~/.config/artcoded-cli.yml, env ARTCODED_CONFIG)
  --json  --yes/-y  --verbose/-v  --no-color
`

// display order, like the menu of the backoffice
var moduleOrder = []string{"home", "tasks", "timesheets", "expenses", "invoices", "dossiers", "documents", "reports", "gallery",
	"cv", "finance", "uploads", "sparql", "prospects", "clients", "mails", "templates", "admin", "toolbox"}

func orderedModules() []*Module {
	rank := map[string]int{}
	for i, n := range moduleOrder {
		rank[n] = i + 1
	}
	out := append([]*Module{}, modules...)
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := rank[out[i].Name], rank[out[j].Name]
		if ri == 0 {
			ri = 1000
		}
		if rj == 0 {
			rj = 1000
		}
		return ri < rj
	})
	return out
}

func printUsage() {
	fmt.Print(usageHeader)
	fmt.Println("\nModules:")
	for _, m := range orderedModules() {
		name := m.Name
		if len(m.Aliases) > 0 {
			name += " (" + strings.Join(m.Aliases, ", ") + ")"
		}
		fmt.Printf("  %-34s %s\n", name, m.Help)
	}
}

func printModuleUsage(m *Module) {
	fmt.Printf("%s: %s\n\nUsage: artcoded-cli %s <action> [arguments] [--flags]\n\nActions:\n", bold(m.Title), m.Help, m.Name)
	for _, a := range m.Actions {
		name := a.Name
		if len(a.Aliases) > 0 {
			name += " (" + strings.Join(a.Aliases, ",") + ")"
		}
		fmt.Printf("  %-28s %s\n", name, a.Help)
		if a.Usage != "" {
			fmt.Printf("  %-28s %s\n", "", dim(m.Name+" "+a.Name+" "+a.Usage))
		}
	}
}

func main() {
	err := run(os.Args[1:])
	if err == nil {
		return
	}
	if errors.Is(err, errCancel) {
		fmt.Fprintln(os.Stderr, dim("cancelled"))
		os.Exit(130)
	}
	fmt.Fprintln(os.Stderr, red("error: ")+err.Error())
	var ae *APIError
	if errors.As(err, &ae) && ae.Status == http.StatusUnauthorized {
		fmt.Fprintln(os.Stderr, "  run: artcoded-cli login")
	}
	os.Exit(1)
}

func run(argv []string) error {
	args := parseArgs(argv)
	if args.flags["no-color"] != nil {
		useColor = false
	}
	cfgPath := defaultConfigPath()
	if v := args.flags["config"]; len(v) > 0 {
		cfgPath = expandPath(v[len(v)-1])
	}
	cmd := ""
	if len(args.pos) > 0 {
		cmd = strings.ToLower(args.pos[0])
		args.pos = args.pos[1:]
	}
	c := &Ctx{args: args, out: os.Stdout}
	c.jsonOut = c.boolFlag("json")
	c.yes = c.boolFlag("yes")
	c.interactive = isInteractive()

	switch cmd {
	case "version", "--version":
		fmt.Println("artcoded-cli " + version)
		return nil
	case "help":
		if t, ok := c.pos(); ok {
			if m := findModule(t); m != nil {
				printModuleUsage(m)
				return nil
			}
		}
		printUsage()
		return nil
	case "init", "setup":
		return runInit(c, cfgPath)
	case "completion":
		shell, _ := c.pos()
		return printCompletion(shell)
	}
	if cmd == "" && c.boolFlag("help") {
		printUsage()
		return nil
	}

	cfg, err := loadConfig(cfgPath)
	if errors.Is(err, os.ErrNotExist) {
		if !c.interactive {
			return fmt.Errorf("no configuration at %s: run 'artcoded-cli init' first", cfgPath)
		}
		fmt.Fprintf(os.Stderr, "No configuration yet (%s), let's create it.\n\n", cfgPath)
		if err := runInit(c, cfgPath); err != nil {
			return err
		}
		if cmd == "" {
			fmt.Fprintln(os.Stderr)
		}
		if cfg, err = loadConfig(cfgPath); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	c.cfg = cfg
	c.api = newAPI(cfg, c.boolFlag("verbose"))

	switch cmd {
	case "":
		return runMenu(c)
	case "login":
		c.api.auth.clearCached()
		t, err := c.api.auth.Login()
		if err != nil {
			return err
		}
		c.api.token = t
		return whoami(c)
	case "logout":
		if err := c.api.auth.Logout(); err != nil {
			return err
		}
		info("logged out")
		return nil
	case "whoami", "me":
		return whoami(c)
	case "config":
		return runConfig(c)
	case "menu":
		return runMenu(c)
	}

	m := findModule(cmd)
	if m == nil {
		return fmt.Errorf("unknown command %q (artcoded-cli help)", cmd)
	}
	actName, _ := c.pos()
	if actName == "" || actName == "help" || c.boolFlag("help") {
		if actName == "" && !c.boolFlag("help") && c.interactive {
			return moduleMenu(c, m, false)
		}
		if a := m.find(actName); a != nil && c.boolFlag("help") {
			fmt.Printf("%s %s %s\n  %s\n", m.Name, a.Name, a.Usage, a.Help)
			return nil
		}
		printModuleUsage(m)
		return nil
	}
	a := m.find(actName)
	if a == nil {
		printModuleUsage(m)
		return fmt.Errorf("unknown action %q for %s", actName, m.Name)
	}
	return a.Run(c)
}

func whoami(c *Ctx) error {
	var me M
	if err := c.api.Get("/api/personal-info/@me", nil, &me); err != nil {
		return err
	}
	return c.result(me, func() {
		details(c.out, "user", str(me, "username"), "email", str(me, "email"), "roles", str(me, "authorities"),
			"backend", c.cfg.BackendURL, "login", c.cfg.Method(), "config", c.cfg.path)
	})
}

func runConfig(c *Ctx) error {
	sub, _ := c.pos()
	switch sub {
	case "", "show":
		m := c.cfg.Map()
		if m["password"] != "" {
			m["password"] = "********"
		}
		if m["client_secret"] != "" {
			m["client_secret"] = "********"
		}
		return c.result(m, func() {
			for _, k := range sortedKeys(m) {
				if m[k] != "" {
					fmt.Fprintf(c.out, "%s: %s\n", dim(k), m[k])
				}
			}
			fmt.Fprintln(c.out, dim("file: "+c.cfg.path))
		})
	case "path":
		fmt.Fprintln(c.out, c.cfg.path)
		return nil
	case "edit":
		data, err := os.ReadFile(c.cfg.path)
		if err != nil {
			return err
		}
		edited, err := editText(c.cfg, string(data), ".yml")
		if err != nil {
			return err
		}
		if err := os.WriteFile(c.cfg.path, []byte(edited), 0o600); err != nil {
			return err
		}
		_, err = loadConfig(c.cfg.path)
		return err
	case "set":
		k, ok1 := c.pos()
		v, ok2 := c.pos()
		if !ok1 || !ok2 {
			return errors.New("usage: config set <key> <value>")
		}
		if err := c.cfg.Set(k, v); err != nil {
			return err
		}
		return c.cfg.Save()
	}
	return fmt.Errorf("unknown config action %q (show, path, edit, set)", sub)
}

// ---------- init ----------

type frontendConfig struct {
	KeycloakURL      string `json:"KEYCLOAK_URL"`
	KeycloakRealm    string `json:"KEYCLOAK_REALM"`
	KeycloakClientID string `json:"KEYCLOAK_CLIENT_ID"`
	BackendURL       string `json:"BACKEND_URL"`
	SparqlEndpoint   string `json:"SPARQL_ENDPOINT"`
}

// the backoffice publishes its runtime config: keycloak settings for free
func discoverFrontend(frontend string) (*frontendConfig, error) {
	u := strings.TrimRight(frontend, "/") + "/assets/config/config.json"
	hc := &http.Client{Timeout: 15 * time.Second}
	res, err := hc.Get(u)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("%s: %s", u, res.Status)
	}
	var fc frontendConfig
	if err := json.NewDecoder(res.Body).Decode(&fc); err != nil {
		return nil, fmt.Errorf("%s: %v", u, err)
	}
	return &fc, nil
}

func normalizeURL(u string) string {
	u = strings.TrimRight(strings.TrimSpace(u), "/")
	if u != "" && !strings.Contains(u, "://") {
		u = "https://" + u
	}
	return u
}

func runInit(c *Ctx, path string) error {
	cfg, err := loadConfig(path)
	if err != nil {
		cfg = &Config{path: path}
	}
	if !c.interactive {
		// non interactive: everything from flags
		for _, ck := range configKeys {
			if v, ok := c.flag(strings.ReplaceAll(ck.key, "_", "-")); ok {
				ck.set(cfg, v)
			}
		}
	} else {
		fmt.Fprintln(os.Stderr, bold("artcoded-cli setup")+dim("  (enter keeps the value in brackets)"))
		v, err := c.opt("frontend-url", "Backoffice url (to read its keycloak settings, optional)", cfg.FrontendURL)
		if err != nil {
			return err
		}
		cfg.FrontendURL = normalizeURL(v)
		if cfg.FrontendURL != "" {
			if fc, err := discoverFrontend(cfg.FrontendURL); err != nil {
				warn("could not read the backoffice config: %v", err)
			} else {
				info("found the keycloak settings of the backoffice")
				if cfg.KeycloakURL == "" {
					cfg.KeycloakURL = fc.KeycloakURL
				}
				if cfg.Realm == "" {
					cfg.Realm = fc.KeycloakRealm
				}
				if cfg.ClientID == "" {
					cfg.ClientID = fc.KeycloakClientID
				}
				if cfg.BackendURL == "" {
					cfg.BackendURL = fc.BackendURL
				}
				if cfg.SparqlEndpoint == "" {
					cfg.SparqlEndpoint = fc.SparqlEndpoint
				}
			}
		}
		steps := []struct {
			flag, label string
			target      *string
			url         bool
		}{
			{"backend-url", "Backend url", &cfg.BackendURL, true},
			{"keycloak-url", "Keycloak url", &cfg.KeycloakURL, true},
			{"realm", "Keycloak realm", &cfg.Realm, false},
			{"client-id", "Keycloak client id", &cfg.ClientID, false},
		}
		for _, s := range steps {
			v, err := c.req(s.flag, s.label, *s.target)
			if err != nil {
				return err
			}
			if s.url {
				v = normalizeURL(v)
			}
			*s.target = v
		}
		methods := []string{"password", "browser", "device"}
		labels := []string{
			"password  username + password stored in the config (keycloak: 'Direct access grants' on the client)",
			"browser   log in with the browser, nothing stored but tokens (keycloak: redirect uri http://127.0.0.1:<port>/*)",
			"device    show a code to confirm in any browser, works over ssh (keycloak: 'Device authorization grant')",
		}
		if v, ok := c.flag("login-method"); ok {
			cfg.LoginMethod = v
		} else {
			i, err := choose("Login method", labels)
			if err != nil {
				return err
			}
			cfg.LoginMethod = methods[i]
		}
		cfg.Username, err = c.opt("username", "Username", cfg.Username)
		if err != nil {
			return err
		}
		if cfg.LoginMethod == "password" {
			if cfg.Username == "" {
				if cfg.Username, err = askRequired("Username", ""); err != nil {
					return err
				}
			}
			if v, ok := c.flag("password"); ok {
				cfg.Password = v
			} else if cfg.PasswordCmd == "" {
				pw, err := askSecret("Password" + dim(" (enter keeps the current one)"))
				if err != nil {
					return err
				}
				if pw != "" {
					cfg.Password = pw
				}
			}
		} else {
			cfg.Password = ""
		}
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	// only saved when the login works
	api := newAPI(cfg, c.boolFlag("verbose"))
	api.auth.clearCached()
	fmt.Fprintln(os.Stderr, dim("logging in..."))
	t, err := api.auth.Login()
	if err != nil {
		return fmt.Errorf("login failed, configuration not saved:\n  %v", err)
	}
	api.token = t
	var me M
	if err := api.Get("/api/personal-info/@me", nil, &me); err != nil {
		return fmt.Errorf("logged in, but the backend refused the token, configuration not saved:\n  %v\n  hint: check backend_url and that the backend trusts this realm (JWK_SET_URI)", err)
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	info("logged in as %s (%s), configuration saved to %s", str(me, "username"), str(me, "authorities"), cfg.path)
	return nil
}

// ---------- interactive menu ----------

type menuEntry struct {
	title  string
	module *Module
	order  float64
}

// the menu configured in the backoffice settings (visible links, the backend already filters by role)
func menuEntries(c *Ctx) ([]menuEntry, error) {
	var links []M
	if err := c.api.Get("/api/settings/menu-link", nil, &links); err != nil {
		return nil, err
	}
	var me M
	_ = c.api.Get("/api/personal-info/@me", nil, &me)
	roles := strList(me, "authorities")
	seen := map[*Module]bool{}
	var out []menuEntry
	routes := map[string]bool{}
	for _, l := range links {
		route := strings.Join(strList(l, "routerLink"), "/")
		routes[route] = true
		if !boolean(l, "show") {
			continue
		}
		for _, m := range modules {
			for _, r := range m.Routes {
				if r == route && !seen[m] {
					seen[m] = true
					out = append(out, menuEntry{title: str(l, "title"), module: m, order: num(l, "order")})
				}
			}
		}
	}
	// pages of the backoffice not in the backend menu (e.g. templates), like withFrontendLinks
	for _, m := range orderedModules() {
		if seen[m] || len(m.FallbackRoles) == 0 {
			continue
		}
		known := false
		for _, r := range m.Routes {
			known = known || routes[r]
		}
		allowed := false
		for _, r := range m.FallbackRoles {
			for _, have := range roles {
				allowed = allowed || r == have
			}
		}
		if !known && allowed {
			seen[m] = true
			out = append(out, menuEntry{title: m.Title, module: m, order: 1000})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].order < out[j].order })
	return out, nil
}

func runMenu(c *Ctx) error {
	if !c.interactive {
		printUsage()
		return nil
	}
	entries, err := menuEntries(c)
	if err != nil {
		return err
	}
	for {
		opts := make([]string, 0, len(entries)+2)
		for _, e := range entries {
			opts = append(opts, fmt.Sprintf("%-18s %s", e.title, dim(e.module.Help)))
		}
		opts = append(opts, fmt.Sprintf("%-18s %s", "Account", dim("whoami, login, logout, configuration")), "Quit")
		fmt.Fprintln(os.Stderr)
		i, err := choose("artcoded", opts)
		if errors.Is(err, errCancel) || i == len(opts)-1 {
			return nil
		}
		if err != nil {
			return err
		}
		if i == len(entries) {
			if err := accountMenu(c); err != nil && !errors.Is(err, errCancel) {
				fmt.Fprintln(os.Stderr, red("error: ")+err.Error())
			}
			continue
		}
		_ = moduleMenu(c, entries[i].module, true)
	}
}

func accountMenu(c *Ctx) error {
	opts := []string{"Who am I", "Log in again", "Log out", "Show configuration", "Edit configuration", "Run setup again"}
	i, err := choose("Account", opts)
	if err != nil {
		return err
	}
	switch i {
	case 0:
		return whoami(c)
	case 1:
		c.api.auth.clearCached()
		t, err := c.api.auth.Login()
		if err != nil {
			return err
		}
		c.api.token = t
		return whoami(c)
	case 2:
		return c.api.auth.Logout()
	case 3:
		c.args.pos = []string{"show"}
		return runConfig(c)
	case 4:
		c.args.pos = []string{"edit"}
		return runConfig(c)
	default:
		return runInit(c, c.cfg.path)
	}
}

// moduleMenu: choose an action of a module, loop when coming from the main menu
func moduleMenu(c *Ctx, m *Module, loop bool) error {
	for {
		opts := make([]string, len(m.Actions))
		for i, a := range m.Actions {
			opts[i] = fmt.Sprintf("%-22s %s", a.Name, dim(a.Help))
		}
		fmt.Fprintln(os.Stderr)
		i, err := choose(m.Title, opts)
		if err != nil {
			if errors.Is(err, errCancel) {
				return nil
			}
			return err
		}
		// a fresh context per action: no leftover arguments
		ac := &Ctx{api: c.api, cfg: c.cfg, args: Args{flags: map[string][]string{}}, out: c.out, interactive: true, jsonOut: c.jsonOut}
		if err := m.Actions[i].Run(ac); err != nil {
			if errors.Is(err, errCancel) {
				fmt.Fprintln(os.Stderr, dim("cancelled"))
			} else {
				fmt.Fprintln(os.Stderr, red("error: ")+err.Error())
			}
		}
		if !loop {
			return nil
		}
	}
}

func printCompletion(shell string) error {
	var mods []string
	cases := ""
	for _, m := range orderedModules() {
		names := append([]string{m.Name}, m.Aliases...)
		mods = append(mods, m.Name)
		var acts []string
		for _, a := range m.Actions {
			acts = append(acts, a.Name)
		}
		cases += fmt.Sprintf("    %s) words=%q ;;\n", strings.Join(names, "|"), strings.Join(acts, " "))
	}
	top := strings.Join(append(mods, "init", "login", "logout", "whoami", "config", "menu", "help", "version", "completion"), " ")
	switch shell {
	case "bash", "":
		fmt.Printf(`_artcoded_cli() {
  local cur=${COMP_WORDS[COMP_CWORD]} words=""
  if [ $COMP_CWORD -eq 1 ]; then
    words=%q
  elif [ $COMP_CWORD -eq 2 ]; then
    case ${COMP_WORDS[1]} in
%s    esac
  fi
  if [[ $cur == -* ]]; then words="--json --yes --out --open --verbose --help --id --page --size"; fi
  COMPREPLY=($(compgen -W "$words" -- "$cur"))
  [ ${#COMPREPLY[@]} -eq 0 ] && COMPREPLY=($(compgen -f -- "$cur"))
}
complete -F _artcoded_cli artcoded-cli
`, top, cases)
	case "zsh":
		fmt.Println("autoload -U +X bashcompinit && bashcompinit")
		return printCompletion("bash")
	default:
		return fmt.Errorf("unknown shell %q (bash or zsh)", shell)
	}
	return nil
}
