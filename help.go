package main

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// Doc of an action: its manual page (artcoded <module> <action> --help)
type Doc struct {
	// what the action does, in a few lines
	Desc string
	// "--name ARG | what it does" ("<arg> | ..." for a positional argument)
	Flags []string
	// "# comment" lines, then the command
	Examples []string
}

type flagDoc struct {
	name, arg, help string
	positional      bool
}

func parseFlagDoc(s string) flagDoc {
	spec, help, _ := strings.Cut(s, "|")
	spec, help = strings.TrimSpace(spec), strings.TrimSpace(help)
	if strings.HasPrefix(spec, "<") || strings.HasPrefix(spec, "[<") {
		return flagDoc{name: spec, help: help, positional: true}
	}
	name, arg, _ := strings.Cut(spec, " ")
	return flagDoc{name: strings.TrimPrefix(name, "--"), arg: strings.TrimSpace(arg), help: help}
}

// global flags, accepted by every action
var globalFlags = []string{
	"--json | print the raw json of the backend (scripts, jq)",
	"--yes | no question: confirms, takes the defaults; a missing required value is an error (alias -y)",
	"--verbose | print the http requests (alias -v)",
	"--config FILE | another configuration file (default ~/.config/artcoded-cli.yml)",
	"--no-color | no colors (also: NO_COLOR=1)",
	"--demo | mask the amounts and the names of clients and people (also demo: true in the config)",
	"--online | open the page of the backoffice showing this instead (list / show actions; needs frontend_url)",
	"--help | this page (alias -h)",
}

func docKey(m *Module, a *Action) string { return m.Name + " " + a.Name }

func docOf(m *Module, a *Action) Doc { return docs[docKey(m, a)] }

// knownFlags: the flags an action documents, plus the global ones
func knownFlags(m *Module, a *Action) map[string]bool {
	known := map[string]bool{}
	for _, f := range append(append([]string{}, globalFlags...), docOf(m, a).Flags...) {
		fd := parseFlagDoc(f)
		known[fd.name] = true // a positional can be given as --name too
		if !fd.positional {
			known["no-"+fd.name] = true
		}
	}
	return known
}

// warnUnknownFlags: a typo (--archive) or a flag of another action
func warnUnknownFlags(m *Module, a *Action, args Args) {
	known := knownFlags(m, a)
	var unknown []string
	for name := range args.flags {
		if !known[name] && !strings.HasPrefix(name, "param-") {
			unknown = append(unknown, "--"+name)
		}
	}
	sort.Strings(unknown)
	if len(unknown) > 0 {
		warn("%s: not a flag of %s %s (artcoded %s %s --help)", strings.Join(unknown, ", "), m.Name, a.Name, m.Name, a.Name)
	}
}

func synopsis(m *Module, a *Action) string {
	parts := []string{"artcoded", m.Name, a.Name}
	for _, f := range docOf(m, a).Flags {
		fd := parseFlagDoc(f)
		if fd.positional {
			parts = append(parts, fd.name)
		}
	}
	if len(docOf(m, a).Flags) > 0 {
		parts = append(parts, "[options]")
	}
	return strings.Join(parts, " ")
}

func indentText(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = prefix + l
		}
	}
	return strings.Join(lines, "\n")
}

func flagTable(b *strings.Builder, flags []string) {
	width := 0
	fds := make([]flagDoc, len(flags))
	for i, f := range flags {
		fds[i] = parseFlagDoc(f)
		w := len(flagLabel(fds[i]))
		if w > width && w <= 28 {
			width = w
		}
	}
	for _, fd := range fds {
		label := flagLabel(fd)
		if len(label) > width {
			fmt.Fprintf(b, "    %s\n    %s  %s\n", bold(label), strings.Repeat(" ", width), fd.help)
			continue
		}
		fmt.Fprintf(b, "    %s%s  %s\n", bold(label), strings.Repeat(" ", width-len(label)), fd.help)
	}
}

func flagLabel(fd flagDoc) string {
	if fd.positional {
		return fd.name
	}
	if fd.arg != "" {
		return "--" + fd.name + " " + fd.arg
	}
	return "--" + fd.name
}

func actionHelp(m *Module, a *Action) string {
	d := docOf(m, a)
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n    artcoded %s %s - %s\n", bold("NAME"), m.Name, a.Name, a.Help)
	if len(a.Aliases) > 0 {
		fmt.Fprintf(&b, "    aliases: %s\n", strings.Join(a.Aliases, ", "))
	}
	fmt.Fprintf(&b, "\n%s\n    %s\n", bold("SYNOPSIS"), synopsis(m, a))
	if d.Desc != "" {
		fmt.Fprintf(&b, "\n%s\n%s\n", bold("DESCRIPTION"), indentText(d.Desc, "    "))
	}
	if len(d.Flags) > 0 {
		fmt.Fprintf(&b, "\n%s\n", bold("OPTIONS"))
		flagTable(&b, d.Flags)
	}
	fmt.Fprintf(&b, "\n%s\n", bold("GLOBAL OPTIONS"))
	flagTable(&b, globalFlags)
	if len(d.Examples) > 0 {
		fmt.Fprintf(&b, "\n%s\n", bold("EXAMPLES"))
		for _, e := range d.Examples {
			for _, line := range strings.Split(e, "\n") {
				if strings.HasPrefix(line, "#") {
					fmt.Fprintf(&b, "    %s\n", dim(line))
				} else {
					fmt.Fprintf(&b, "    %s\n", line)
				}
			}
			b.WriteString("\n")
		}
	}
	fmt.Fprintf(&b, "%s\n    artcoded %s --help (the other actions of %s)\n", bold("SEE ALSO"), m.Name, m.Name)
	return b.String()
}

func moduleHelp(m *Module) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n    artcoded %s - %s\n", bold("NAME"), m.Name, m.Help)
	if len(m.Aliases) > 0 {
		fmt.Fprintf(&b, "    aliases: %s\n", strings.Join(m.Aliases, ", "))
	}
	fmt.Fprintf(&b, "\n%s\n    artcoded %s <action> [arguments] [options]\n    artcoded %s <action> --help\n\n%s\n", bold("SYNOPSIS"), m.Name, m.Name, bold("ACTIONS"))
	width := 0
	for _, a := range m.Actions {
		width = max(width, len(a.Name))
	}
	for _, a := range m.Actions {
		fmt.Fprintf(&b, "    %s%s  %s\n", bold(a.Name), strings.Repeat(" ", width-len(a.Name)), a.Help)
	}
	if len(m.Actions) > 0 {
		a := m.Actions[0]
		if ex := docOf(m, a).Examples; len(ex) > 0 {
			fmt.Fprintf(&b, "\n%s\n%s\n", bold("EXAMPLE"), indentText(ex[0], "    "))
		}
	}
	fmt.Fprintf(&b, "\nInteractive menu of this module: artcoded menu %s\n", m.Name)
	return b.String()
}

// page: through $PAGER (less) when the text is longer than the terminal
func page(text string) {
	if bot != nil || !isTerminal(os.Stdout) || strings.Count(text, "\n") < terminalLines()-2 {
		fmt.Fprint(stdout, text)
		return
	}
	pager := os.Getenv("PAGER")
	if pager == "" {
		pager = "less -FRX"
	}
	parts := strings.Fields(pager)
	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Stdin = strings.NewReader(text)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprint(stdout, text)
	}
}

func terminalLines() int {
	if out, err := stty("size"); err == nil {
		var rows, cols int
		if _, err := fmt.Sscanf(out, "%d %d", &rows, &cols); err == nil && rows > 0 {
			return rows
		}
	}
	return 40
}
