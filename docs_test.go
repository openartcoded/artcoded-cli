package main

import (
	"strings"
	"testing"
)

func TestEveryActionHasAManualPage(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range modules {
		for _, a := range m.Actions {
			key := docKey(m, a)
			seen[key] = true
			d, ok := docs[key]
			if !ok {
				t.Errorf("%s: no doc", key)
				continue
			}
			if strings.TrimSpace(d.Desc) == "" || len(d.Examples) == 0 {
				t.Errorf("%s: a description and an example are required", key)
			}
		}
	}
	for key := range docs {
		if !seen[key] {
			t.Errorf("doc of an unknown action: %s", key)
		}
	}
}

func TestExamplesUseDocumentedFlags(t *testing.T) {
	for _, m := range modules {
		for _, a := range m.Actions {
			known := knownFlags(m, a)
			for _, ex := range docOf(m, a).Examples {
				for _, line := range strings.Split(ex, "\n") {
					if strings.HasPrefix(line, "#") {
						continue
					}
					words := shellWords(line)
					for i := 0; i < len(words); i++ {
						if words[i] != "artcoded" {
							continue
						}
						var args []string
						for j := i + 1; j < len(words) && !strings.ContainsAny(words[j], "|>;&"); j++ {
							args = append(args, words[j])
						}
						if len(args) > 0 && (args[0] == "dashboard" || args[0] == "dash") {
							args = append([]string{"home", "dashboard"}, args[1:]...)
						}
						if len(args) < 2 {
							t.Errorf("%s: example %q does not name an action", docKey(m, a), line)
							continue
						}
						em, ea := findModule(args[0]), (*Action)(nil)
						if em != nil {
							ea = em.find(args[1])
						}
						if ea == nil {
							t.Errorf("%s: example %q: unknown action", docKey(m, a), line)
							continue
						}
						ek := knownFlags(em, ea)
						if em == m && ea == a {
							ek = known
						}
						for name := range parseArgs(args[2:]).flags {
							if !ek[name] {
								t.Errorf("%s: example %q uses --%s, not documented for %s", docKey(m, a), line, name, docKey(em, ea))
							}
						}
					}
				}
			}
		}
	}
}

func TestHelpPages(t *testing.T) {
	m := findModule("expenses")
	page := actionHelp(m, m.find("list"))
	for _, want := range []string{"NAME", "SYNOPSIS", "DESCRIPTION", "OPTIONS", "--processed", "EXAMPLES", "artcoded expenses list --processed"} {
		if !strings.Contains(page, want) {
			t.Errorf("expenses list --help misses %q", want)
		}
	}
	if !strings.Contains(moduleHelp(m), "labels-edit") {
		t.Error("module help lists the actions")
	}
}
