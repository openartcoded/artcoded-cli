package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime/debug"
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

const usageHeader = `artcoded: the artcoded backoffice in your terminal

Usage:
  artcoded dashboard                      the most useful figures at a glance
  artcoded <module> <action> [arguments] [--flags]
  artcoded <module> --help            the actions of a module
  artcoded <module> <action> --help   manual of an action: options and examples
  artcoded menu [<module>]            interactive menu (the one of the backoffice settings)

Missing arguments are asked in a terminal. Use --yes to skip questions/confirmations
(scripts), --json for the raw json, --out <file|dir> and --open for downloads.

Setup:
  init                   create / change the configuration and log in
  login | logout | whoami
  reset                  forget the configuration and the session (then: init)
  update                 install the latest release (checked once a day; DISABLE_AUTO_UPDATE=true: never)
  config [show|path|edit|set <key> <value>|reset]
  completion bash|zsh    shell completion: source <(artcoded completion bash)
  bot [--console]        the WhatsApp bot (ENABLE_WHATSAPP_BOT=true), artcoded bot --help

Global flags:
  --config <path>        configuration file (default ~/.config/artcoded-cli.yml, env ARTCODED_CONFIG)
  --online               open the page of the backoffice instead (list / show actions)
  --demo                 mask the amounts and names (screen sharing)
  --json  --yes/-y  --verbose/-v  --no-color  --version/-V
`

// display order, like the menu of the backoffice
var moduleOrder = []string{"home", "stats", "tasks", "timesheets", "expenses", "invoices", "dossiers", "documents", "reports", "gallery",
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

const chatUsageHeader = `*artcoded*: write the commands of the cli, e.g.
• dashboard
• invoices list
• expenses list --processed
• _<module> help_: the actions of a module
• _<module> <action> --help_: the manual of an action

Missing arguments are asked: answer in the chat, *.* for the default, *cancel* to stop.
Send a file: the bot asks what it is for (or say it in the caption: expense, document...).
`

func printUsage(cfgPath string) {
	if bot != nil {
		fmt.Fprint(stdout, chatUsageHeader)
		fmt.Fprintln(stdout, "\n*Modules*")
		visible := visibleModules(cfgPath)
		for _, m := range orderedModules() {
			if visible == nil || visible[m] {
				fmt.Fprintf(stdout, "• *%s*: %s\n", m.Name, m.Help)
			}
		}
		return
	} else {
		fmt.Fprint(stdout, usageHeader)
	}
	visible := visibleModules(cfgPath)
	fmt.Fprintln(stdout, "\nModules (artcoded <module> --help):")
	for _, m := range orderedModules() {
		// the ones the menu of the backoffice hides are hidden here too
		if visible != nil && !visible[m] {
			continue
		}
		name := m.Name
		if len(m.Aliases) > 0 {
			name += " (" + strings.Join(m.Aliases, ", ") + ")"
		}
		fmt.Fprintf(stdout, "  %-34s %s\n", name, m.Help)
	}
}

func main() {
	err := run(os.Args[1:])
	if err == nil {
		return
	}
	if errors.Is(err, errCancel) {
		fmt.Fprintln(stderr, dim("cancelled"))
		os.Exit(130)
	}
	fmt.Fprintln(stderr, red("error: ")+err.Error())
	var ae *APIError
	if errors.As(err, &ae) && ae.Status == http.StatusUnauthorized {
		fmt.Fprintln(stderr, "  run: artcoded login")
	}
	if isCertError(err) {
		fmt.Fprintln(stderr, "  hint: "+certHint)
	}
	os.Exit(1)
}

func run(argv []string) error {
	args := parseArgs(argv)
	// artcoded dashboard = artcoded home dashboard
	if len(args.pos) > 0 && (args.pos[0] == "dashboard" || args.pos[0] == "dash") {
		args.pos = append([]string{"home", "dashboard"}, args.pos[1:]...)
	}
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
	c := &Ctx{args: args, out: stdout}
	c.jsonOut = c.boolFlag("json")
	c.yes = c.boolFlag("yes")
	c.interactive = isInteractive()
	demoMode = c.boolFlag("demo")

	if c.boolFlag("version") && cmd == "" {
		cmd = "version"
	}
	switch cmd {
	case "version":
		fmt.Fprintln(stdout, "artcoded "+versionString())
		return nil
	case "help", "man":
		if t, ok := c.pos(); ok {
			m := findModule(t)
			if m == nil {
				return fmt.Errorf("unknown module %q (artcoded help)", t)
			}
			if an, ok := c.pos(); ok {
				a := m.find(an)
				if a == nil {
					return fmt.Errorf("unknown action %q for %s (artcoded %s --help)", an, m.Name, m.Name)
				}
				page(actionHelp(m, a))
				return nil
			}
			page(moduleHelp(m))
			return nil
		}
		printUsage(cfgPath)
		return nil
	case "init", "setup":
		return runInit(c, cfgPath)
	case "completion":
		shell, _ := c.pos()
		return printCompletion(shell)
	case "reset":
		return runReset(c, cfgPath)
	case "update", "upgrade":
		return runUpdate(c)
	case "bot":
		return runBot(c, cfgPath)
	}
	maybeAutoUpdate(cmd)
	if cmd == "" {
		// the menu only when asked: artcoded menu
		printUsage(cfgPath)
		if _, err := os.Stat(cfgPath); err != nil {
			fmt.Fprintln(stdout, "\nFirst time? Run: artcoded init")
		}
		return nil
	}
	// help never needs the configuration nor a login
	var m *Module
	var act *Action
	if cmd != "menu" {
		m = findModule(cmd)
	}
	if m != nil {
		actName := ""
		if len(c.args.pos) > 0 {
			actName = c.args.pos[0]
		}
		if actName == "" || actName == "help" {
			page(moduleHelp(m))
			return nil
		}
		act = m.find(actName)
		if act == nil {
			fmt.Fprint(stdout, moduleHelp(m))
			return fmt.Errorf("unknown action %q for %s", actName, m.Name)
		}
		c.args.pos = c.args.pos[1:]
		if c.boolFlag("help") {
			page(actionHelp(m, act))
			return nil
		}
		warnUnknownFlags(m, act, c.args)
	} else if cmd != "menu" && cmd != "login" && cmd != "logout" && cmd != "whoami" && cmd != "me" && cmd != "config" {
		return fmt.Errorf("unknown command %q (artcoded help)", cmd)
	}

	cfg, err := loadConfig(cfgPath)
	if errors.Is(err, os.ErrNotExist) {
		if !c.interactive {
			return fmt.Errorf("no configuration at %s: run 'artcoded init' first", cfgPath)
		}
		fmt.Fprintf(stderr, "No configuration yet (%s), let's create it.\n\n", cfgPath)
		if err := runInit(c, cfgPath); err != nil {
			return err
		}
		if cmd == "" {
			fmt.Fprintln(stderr)
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
	if !c.has("demo") {
		demoMode = cfg.Demo
	}
	if demoMode && c.jsonOut {
		return errors.New("--json shows the raw data: not in demo mode (--demo / demo: true in the config)")
	}

	switch cmd {
	case "login":
		c.api.auth.clearCached()
		t, err := c.api.auth.Login()
		if err != nil {
			return err
		}
		c.api.token = t
		var me M
		if c.api.Get("/api/personal-info/@me", nil, &me) == nil {
			refreshMenuCache(c.api, cfg, strList(me, "authorities"))
		}
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
		if !c.interactive {
			return errors.New("the menu needs a terminal")
		}
		if name, ok := c.pos(); ok {
			mm := findModule(name)
			if mm == nil {
				return fmt.Errorf("unknown module %q", name)
			}
			return moduleMenu(c, mm, true)
		}
		return runMenu(c)
	}
	if c.boolFlag("online") {
		return openOnline(c, m, act)
	}
	return act.Run(c)
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
	case "reset":
		return runReset(c, c.cfg.path)
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
	return fmt.Errorf("unknown config action %q (show, path, edit, set, reset)", sub)
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
func discoverFrontend(cfg *Config, frontend string) (*frontendConfig, error) {
	u := strings.TrimRight(frontend, "/") + "/assets/config/config.json"
	hc := httpClient(cfg, 15*time.Second)
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
		fmt.Fprintln(stderr, bold("artcoded setup")+dim("  (enter keeps the value in brackets)"))
		v, err := c.opt("frontend-url", "Backoffice url (to read its keycloak settings, optional)", cfg.FrontendURL)
		if err != nil {
			return err
		}
		cfg.FrontendURL = normalizeURL(v)
		if cfg.FrontendURL != "" {
			fc, err := discoverFrontend(cfg, cfg.FrontendURL)
			if isCertError(err) {
				if ok, aerr := askCertTrust(cfg, err); aerr != nil {
					return aerr
				} else if ok {
					fc, err = discoverFrontend(cfg, cfg.FrontendURL)
				}
			}
			if err != nil {
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
	var api *API
	var me M
	for {
		api = newAPI(cfg, c.boolFlag("verbose"))
		api.auth.clearCached()
		fmt.Fprintln(stderr, dim("logging in..."))
		t, err := api.auth.Login()
		if err == nil {
			api.token = t
			err = api.Get("/api/personal-info/@me", nil, &me)
		}
		if err == nil {
			break
		}
		if isCertError(err) && !cfg.Insecure {
			if ok, aerr := askCertTrust(cfg, err); aerr != nil {
				return aerr
			} else if ok {
				continue
			}
			return fmt.Errorf("configuration not saved:\n  %v\n  hint: %s", err, certHint)
		}
		if api.token == nil {
			return fmt.Errorf("login failed, configuration not saved:\n  %v", err)
		}
		return fmt.Errorf("logged in, but the backend refused the token, configuration not saved:\n  %v\n  hint: check backend_url and that the backend trusts this realm (JWK_SET_URI)", err)
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	refreshMenuCache(api, cfg, strList(me, "authorities"))
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
	saveMenuCache(c.cfg, menuCache{Links: links, Roles: roles})
	return entriesOf(links, roles), nil
}

// refreshMenuCache reads the menu of the backend for the module list of the usage
func refreshMenuCache(api *API, cfg *Config, roles []string) {
	var links []M
	if api.Get("/api/settings/menu-link", nil, &links) == nil {
		saveMenuCache(cfg, menuCache{Links: links, Roles: roles})
	}
}

// entriesOf: the modules the menu of the backoffice shows (visible links, frontend only pages for the roles)
func entriesOf(links []M, roles []string) []menuEntry {
	seen := map[*Module]bool{}
	var out []menuEntry
	routes := map[string]bool{}
	for _, l := range links {
		route := strings.Join(strList(l, "routerLink"), "/")
		routes[route] = true
		if !boolean(l, "show") {
			continue
		}
		// a link can open several modules (home: the dashboard and the statistics, administration: the toolbox):
		// the first one takes the title of the link, the others follow it
		n := 0
		for _, m := range orderedModules() {
			for _, r := range m.Routes {
				if r == route && !seen[m] {
					seen[m] = true
					title := str(l, "title")
					if n > 0 {
						title = m.Title
					}
					out = append(out, menuEntry{title: title, module: m, order: num(l, "order") + float64(n)/100})
					n++
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
	return out
}

// the menu last read from the backend: the usage lists only these modules, without a request
type menuCache struct {
	Links []M      `json:"links"`
	Roles []string `json:"roles"`
}

func menuCachePath(cfg *Config) string {
	return strings.Replace(tokenCachePath(cfg), "tokens-", "menu-", 1)
}

func saveMenuCache(cfg *Config, mc menuCache) {
	if data, err := json.Marshal(mc); err == nil {
		_ = os.WriteFile(menuCachePath(cfg), data, 0o600)
	}
}

// visibleModules: the modules of the menu of the backoffice (nil: unknown, every module)
func visibleModules(cfgPath string) map[*Module]bool {
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(menuCachePath(cfg))
	if err != nil {
		return nil
	}
	var mc menuCache
	if json.Unmarshal(data, &mc) != nil || len(mc.Links) == 0 {
		return nil
	}
	set := map[*Module]bool{}
	for _, e := range entriesOf(mc.Links, mc.Roles) {
		set[e.module] = true
	}
	return set
}

func runMenu(c *Ctx) error {
	if !c.interactive {
		printUsage(c.cfg.path)
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
		fmt.Fprintln(stderr)
		i, err := choose("artcoded", opts)
		if errors.Is(err, errCancel) || i == len(opts)-1 {
			return nil
		}
		if err != nil {
			return err
		}
		if i == len(entries) {
			if err := accountMenu(c); err != nil && !errors.Is(err, errCancel) {
				fmt.Fprintln(stderr, red("error: ")+err.Error())
			}
			continue
		}
		_ = moduleMenu(c, entries[i].module, true)
	}
}

func accountMenu(c *Ctx) error {
	opts := []string{"Who am I", "Log in again", "Log out", "Show configuration", "Edit configuration", "Run setup again", "Forget the configuration (reset)"}
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
	case 5:
		return runInit(c, c.cfg.path)
	default:
		return runReset(c, c.cfg.path)
	}
}

// moduleMenu: choose an action of a module, loop when coming from the main menu
func moduleMenu(c *Ctx, m *Module, loop bool) error {
	for {
		opts := make([]string, len(m.Actions))
		for i, a := range m.Actions {
			opts[i] = fmt.Sprintf("%-22s %s", a.Name, dim(a.Help))
		}
		fmt.Fprintln(stderr)
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
				fmt.Fprintln(stderr, dim("cancelled"))
			} else {
				fmt.Fprintln(stderr, red("error: ")+err.Error())
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
	top := strings.Join(append(mods, "init", "update", "reset", "login", "logout", "whoami", "config", "menu", "help", "version", "completion", "bot"), " ")
	switch shell {
	case "bash", "":
		fmt.Fprintf(stdout, `_artcoded() {
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
complete -F _artcoded artcoded
`, top, cases)
	case "zsh":
		fmt.Fprintln(stdout, "autoload -U +X bashcompinit && bashcompinit")
		return printCompletion("bash")
	default:
		return fmt.Errorf("unknown shell %q (bash or zsh)", shell)
	}
	return nil
}

// runReset forgets everything: the session (logged out of keycloak), the cached tokens and the configuration file.
// Then the setup can start again.
func runReset(c *Ctx, path string) error {
	cfg, err := loadConfig(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			info("no configuration at %s", path)
			if c.interactive && !c.yes && askBool("Run the setup now?", true) {
				return runInit(c, path)
			}
			return nil
		}
		// unreadable: it can still be removed
		cfg = nil
	}
	if err := c.confirmAction("Forget the configuration " + path + " and log out?"); err != nil {
		return err
	}
	if cfg != nil {
		auth := &Auth{cfg: cfg, http: httpClient(cfg, 15*time.Second)}
		if err := auth.Logout(); err != nil {
			warn("could not log out of keycloak (%v): the local session is removed anyway", err)
		}
		auth.clearCached()
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	info("configuration and session removed")
	if c.interactive && !c.yes && askBool("Run the setup now?", true) {
		fmt.Fprintln(stderr)
		return runInit(c, path)
	}
	fmt.Fprintln(stdout, "run: artcoded init")
	return nil
}

// versionString: the release version, else what go knows of the build (go install, a git checkout)
func versionString() string {
	if version != "dev" {
		return version
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return version
	}
	if v := bi.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	rev, dirty := "", false
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return version
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	if dirty {
		rev += "-dirty"
	}
	return "dev-" + rev
}
