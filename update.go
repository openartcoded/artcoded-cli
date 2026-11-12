package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Auto-update: once a day, in a terminal, artcoded checks the latest release on GitHub and offers to
// install it with install.sh. DISABLE_AUTO_UPDATE=true turns it off; never in scripts, the bot, docker,
// or on a dev / edge build.

const defaultRepo = "openartcoded/artcoded-cli"

func releaseRepo() string {
	if r := os.Getenv("ARTCODED_REPO"); r != "" {
		return r
	}
	return defaultRepo
}

// semver of a release tag (v2027.0.3); ok false for dev / edge builds
func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if !strings.HasPrefix(v, "v") || len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

func newer(latest, current string) bool {
	l, ok1 := parseVersion(latest)
	c, ok2 := parseVersion(current)
	if !ok1 || !ok2 {
		return false
	}
	for i := 0; i < 3; i++ {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

func updateCheckPath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "artcoded-cli", "update-check")
}

// latestRelease: the tag of the latest stable release (GitHub api, short timeout)
func latestRelease(timeout time.Duration) (string, error) {
	hc := &http.Client{Timeout: timeout}
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/"+releaseRepo()+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github: %s", res.Status)
	}
	var r struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
		return "", err
	}
	return r.TagName, nil
}

func autoUpdateDisabled() bool {
	v, _ := parseBool(os.Getenv("DISABLE_AUTO_UPDATE"))
	return v
}

// maybeAutoUpdate: at most once a day, before a command run in a terminal
func maybeAutoUpdate(cmd string) {
	switch cmd {
	case "bot", "completion", "version", "update":
		return
	}
	if autoUpdateDisabled() || !isInteractive() || !isTerminal(os.Stdout) {
		return
	}
	if _, ok := parseVersion(version); !ok {
		return // dev / edge build
	}
	stamp := updateCheckPath()
	if st, err := os.Stat(stamp); err == nil && time.Since(st.ModTime()) < 24*time.Hour {
		return
	}
	_ = os.MkdirAll(filepath.Dir(stamp), 0o700)
	_ = os.WriteFile(stamp, []byte(time.Now().Format(time.RFC3339)), 0o600)
	latest, err := latestRelease(2 * time.Second)
	if err != nil || !newer(latest, version) {
		return
	}
	ok, err := confirm(fmt.Sprintf("artcoded %s is out (you have %s). Update now?", latest, version), false)
	if err != nil || !ok {
		fmt.Fprintln(os.Stderr, dim("  later: artcoded update · never: DISABLE_AUTO_UPDATE=true"))
		return
	}
	if err := installRelease(latest); err != nil {
		warn("update failed: %v", err)
		return
	}
	info("updated to %s: run your command again", latest)
	os.Exit(0)
}

// installRelease runs install.sh for this version, into the directory of the running binary
func installRelease(tag string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	script := fmt.Sprintf("curl -fsSL https://raw.githubusercontent.com/%s/main/install.sh | sh -s -- %s", releaseRepo(), tag)
	fmt.Fprintln(os.Stderr, dim("$ "+script))
	cmd := exec.Command("sh", "-c", script)
	cmd.Env = append(os.Environ(), "ARTCODED_INSTALL_DIR="+filepath.Dir(exe), "ARTCODED_REPO="+releaseRepo())
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stderr, os.Stderr
	return cmd.Run()
}

// runUpdate: artcoded update, now (no daily limit)
func runUpdate(c *Ctx) error {
	latest, err := latestRelease(15 * time.Second)
	if err != nil {
		return fmt.Errorf("cannot read the latest release: %w", err)
	}
	if _, ok := parseVersion(version); ok && !newer(latest, version) && !c.boolFlag("force") {
		info("up to date (%s)", version)
		return nil
	}
	if err := c.confirmAction(fmt.Sprintf("Install artcoded %s (you have %s)?", latest, versionString())); err != nil {
		return err
	}
	if err := installRelease(latest); err != nil {
		return err
	}
	info("updated to %s", latest)
	return nil
}
