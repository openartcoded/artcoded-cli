package main

import "strings"

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
