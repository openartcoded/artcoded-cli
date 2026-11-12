package main

import (
	"strings"
	"testing"
)

// shell-like words of an example: quotes kept together, operators (| && > ;) as their own word
func shellWords(line string) []string {
	var words []string
	var cur strings.Builder
	quote := rune(0)
	flush := func() {
		if cur.Len() > 0 {
			words = append(words, cur.String())
			cur.Reset()
		}
	}
	rs := []rune(line)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote = r
		case r == ' ':
			flush()
		case r == '|' || r == '>' || r == ';':
			flush()
			words = append(words, string(r))
		case r == '&' && i+1 < len(rs) && rs[i+1] == '&':
			flush()
			words = append(words, "&&")
			i++
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return words
}

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
