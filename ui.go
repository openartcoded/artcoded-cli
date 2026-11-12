package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

var errCancel = errors.New("cancelled")

// where the commands write: the terminal, or the conversation of the bot
var (
	stdout io.Writer = os.Stdout
	stderr io.Writer = os.Stderr
)

var stdin = bufio.NewReader(os.Stdin)

func isWindows() bool { return runtime.GOOS == "windows" }

func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

var forceNonInteractive bool

func isInteractive() bool {
	return forceChatInteractive || !forceNonInteractive && isTerminal(os.Stdin)
}

var useColor = os.Getenv("NO_COLOR") == "" && isTerminal(os.Stdout) && os.Getenv("TERM") != "dumb"

func ansi(code, s string) string {
	if !useColor {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}
func bold(s string) string {
	if bot != nil {
		return chatStyle(s, "*")
	}
	return ansi("1", s)
}

func dim(s string) string {
	if bot != nil {
		return chatStyle(s, "_")
	}
	return ansi("2", s)
}
func green(s string) string  { return ansi("32", s) }
func red(s string) string    { return ansi("31", s) }
func yellow(s string) string { return ansi("33", s) }
func cyan(s string) string   { return ansi("36", s) }

func info(format string, a ...any) { fmt.Fprintln(stderr, green("✓ ")+fmt.Sprintf(format, a...)) }
func warn(format string, a ...any) { fmt.Fprintln(stderr, yellow("! ")+fmt.Sprintf(format, a...)) }

// ask a value, def shown in brackets and returned on enter.
func ask(label, def string) (string, error) {
	if bot != nil {
		printPrompt(bold(label))
		chatHint = ". for nothing"
		if def != "" {
			chatHint = ". keeps “" + truncate(def, 60) + "”"
		}
	} else if def != "" {
		printPrompt(fmt.Sprintf("%s %s: ", bold(label), dim("["+def+"]")))
	} else {
		printPrompt(fmt.Sprintf("%s: ", bold(label)))
	}
	v, err := readLine()
	if err != nil {
		return "", err
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return def, nil
	}
	return v, nil
}

func askRequired(label, def string) (string, error) {
	for {
		v, err := ask(label, def)
		if err != nil || v != "" {
			return v, err
		}
		fmt.Fprintln(stderr, red("  required"))
	}
}

func askSecret(label string) (string, error) {
	printPrompt(fmt.Sprintf("%s: ", bold(label)))
	return readLineMode(true)
}

func confirm(label string, def bool) (bool, error) {
	hint := "y/N"
	if def {
		hint = "Y/n"
	}
	for {
		chatHint = "yes or no"
		printPrompt(fmt.Sprintf("%s %s ", bold(label), dim("["+hint+"]")))
		v, err := readLine()
		if err != nil {
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "":
			return def, nil
		case "y", "yes", "o", "oui", "j", "ja", "true":
			return true, nil
		case "n", "no", "non", "nee", "false":
			return false, nil
		}
	}
}

// choose one option. Type a number, or text to filter long lists. Returns the index.
func choose(label string, options []string) (int, error) {
	if len(options) == 0 {
		return -1, errors.New("nothing to choose from")
	}
	idx := make([]int, len(options))
	for i := range idx {
		idx[i] = i
	}
	filter := ""
	for {
		shown := idx
		if filter != "" {
			shown = nil
			f := strings.ToLower(filter)
			for i, o := range options {
				if strings.Contains(strings.ToLower(o), f) {
					shown = append(shown, i)
				}
			}
			if len(shown) == 0 {
				fmt.Fprintln(stderr, red("  no match for "+strconv.Quote(filter)))
				filter = ""
				continue
			}
			if len(shown) == 1 {
				fmt.Fprintf(stderr, "  → %s\n", options[shown[0]])
				return shown[0], nil
			}
		}
		fmt.Fprintln(stderr, bold(label))
		const pageSize = 40
		for n, i := range shown {
			if n >= pageSize {
				fmt.Fprintf(stderr, dim("  … %d more, type text to filter\n"), len(shown)-pageSize)
				break
			}
			fmt.Fprintln(stderr, optionLine(n+1, options[i]))
		}
		hint := "number"
		if len(shown) > 8 {
			hint = "number or text to filter"
		}
		if bot != nil {
			chatHint = "a " + hint
		} else {
			printPrompt(fmt.Sprintf("%s ", dim("> "+hint+", empty to cancel:")))
		}
		v, err := readLine()
		if err != nil {
			return -1, err
		}
		v = strings.TrimSpace(v)
		if v == "" {
			if filter != "" {
				filter = ""
				continue
			}
			return -1, errCancel
		}
		if n, err := strconv.Atoi(v); err == nil {
			if n >= 1 && n <= len(shown) {
				return shown[n-1], nil
			}
			fmt.Fprintln(stderr, red("  out of range"))
			continue
		}
		filter = v
	}
}

var spacesRe = regexp.MustCompile(` {2,}`)

// optionLine: "  3) text"; in a chat "*3.* text", the aligned columns of the text as " · "
func optionLine(n int, text string) string {
	if bot != nil {
		return fmt.Sprintf("*%d.* %s", n, spacesRe.ReplaceAllString(strings.TrimSpace(stripANSI(text)), " · "))
	}
	return fmt.Sprintf("  %s %s", cyan(fmt.Sprintf("%3d)", n)), text)
}

// chooseMany: "1,3,5-7", "all", or text to filter first.
func chooseMany(label string, options []string) ([]int, error) {
	if len(options) == 0 {
		return nil, errors.New("nothing to choose from")
	}
	fmt.Fprintln(stderr, bold(label))
	for i, o := range options {
		fmt.Fprintln(stderr, optionLine(i+1, o))
	}
	for {
		if bot != nil {
			chatHint = "numbers like 1,3,5-7, or all"
		} else {
			printPrompt(fmt.Sprintf("%s ", dim("> e.g. 1,3,5-7 or all, empty to cancel:")))
		}
		v, err := readLine()
		if err != nil {
			return nil, err
		}
		v = strings.TrimSpace(v)
		if v == "" {
			return nil, errCancel
		}
		if strings.EqualFold(v, "all") || v == "*" {
			out := make([]int, len(options))
			for i := range out {
				out[i] = i
			}
			return out, nil
		}
		sel, err := parseRanges(v, len(options))
		if err != nil {
			fmt.Fprintln(stderr, red("  "+err.Error()))
			continue
		}
		return sel, nil
	}
}

func parseRanges(v string, n int) ([]int, error) {
	seen := map[int]bool{}
	var out []int
	for _, part := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' }) {
		lo, hi, isRange := strings.Cut(part, "-")
		a, err := strconv.Atoi(lo)
		if err != nil {
			return nil, fmt.Errorf("not a number: %s", part)
		}
		b := a
		if isRange {
			if b, err = strconv.Atoi(hi); err != nil {
				return nil, fmt.Errorf("not a range: %s", part)
			}
		}
		if a < 1 || b > n || a > b {
			return nil, fmt.Errorf("out of range: %s", part)
		}
		for i := a; i <= b; i++ {
			if !seen[i] {
				seen[i] = true
				out = append(out, i-1)
			}
		}
	}
	return out, nil
}

func editorCommand(cfg *Config) string {
	for _, e := range []string{cfgEditor(cfg), os.Getenv("VISUAL"), os.Getenv("EDITOR")} {
		if e != "" {
			return e
		}
	}
	if isWindows() {
		return "notepad"
	}
	for _, e := range []string{"nano", "vim", "vi"} {
		if _, err := exec.LookPath(e); err == nil {
			return e
		}
	}
	return "vi"
}

func cfgEditor(cfg *Config) string {
	if cfg == nil {
		return ""
	}
	return cfg.Editor
}

// editText opens the editor on initial text, returns the edited text.
func editText(cfg *Config, initial, ext string) (string, error) {
	if bot != nil {
		return "", errors.New("no editor on WhatsApp: pass the value as a flag (or a file)")
	}
	if !isInteractive() {
		return "", errors.New("an editor is needed: run in a terminal or pass the value as a flag / file")
	}
	f, err := os.CreateTemp("", "artcoded-*"+ext)
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(initial); err != nil {
		return "", err
	}
	f.Close()
	parts := strings.Fields(editorCommand(cfg))
	cmd := exec.Command(parts[0], append(parts[1:], f.Name())...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("editor %s: %w", parts[0], err)
	}
	data, err := os.ReadFile(f.Name())
	return string(data), err
}

// editJSON lets the user edit v as json; asks again on a syntax error.
func editJSON(cfg *Config, v any, out any) (bool, error) {
	initial, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return false, err
	}
	text := string(initial)
	for {
		edited, err := editText(cfg, text, ".json")
		if err != nil {
			return false, err
		}
		if strings.TrimSpace(edited) == strings.TrimSpace(string(initial)) {
			return false, nil
		}
		if err := json.Unmarshal([]byte(edited), out); err != nil {
			fmt.Fprintln(stderr, red("invalid json: "+err.Error()))
			again, cerr := confirm("Edit again?", true)
			if cerr != nil || !again {
				return false, errCancel
			}
			text = edited
			continue
		}
		return true, nil
	}
}

// openInBrowser opens a url or a file: $BROWSER when set (every system, the usual convention),
// else the opener of the system.
func openInBrowser(target string) error {
	if bot != nil {
		if strings.Contains(target, "://") {
			return bot.sendLink(target)
		}
		data, err := os.ReadFile(target)
		if err != nil {
			return err
		}
		return bot.sendFile(filepath.Base(target), contentTypeFor(target, data), data)
	}
	var cmd *exec.Cmd
	if b := strings.Fields(os.Getenv("BROWSER")); len(b) > 0 {
		cmd = exec.Command(b[0], append(b[1:], target)...)
	} else {
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", target)
		case "windows":
			cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
		default:
			if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
				return errors.New("no display")
			}
			cmd = exec.Command("xdg-open", target)
		}
	}
	cmd.Stdout, cmd.Stderr = nil, nil
	return cmd.Start()
}

// details prints key / value lines (empty values skipped)
func details(w io.Writer, kv ...string) {
	if bot != nil {
		chatDetails(w, kv...)
		return
	}
	width := 0
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] != "" && len(kv[i]) > width {
			width = len(kv[i])
		}
	}
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] == "" {
			continue
		}
		fmt.Fprintf(w, "%s  %s\n", dim(fmt.Sprintf("%-*s", width, kv[i])), kv[i+1])
	}
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

func expandPath(p string) string {
	p = strings.TrimSpace(p)
	if len(p) >= 2 && (p[0] == '"' && p[len(p)-1] == '"' || p[0] == '\'' && p[len(p)-1] == '\'') {
		p = p[1 : len(p)-1]
	}
	p = strings.ReplaceAll(p, `\ `, " ")
	if strings.HasPrefix(p, "~/") || p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
