package aggregate

import "strings"

// Manifest is what the board reads of a repo's relai.yaml beyond its id and summary: what it provides and
// consumes (capability names) and the ids of the repos it says it works with.
type Manifest struct {
	Provides []string `json:"provides,omitempty"`
	Consumes []string `json:"consumes,omitempty"`
	Links    []string `json:"links,omitempty"`
}

// parseLists reads the provides:, consumes: and links: lists of a relai.yaml. It accepts the shapes people
// write: a block of "- capability: x" or "- x" lines (comments and blank lines inside are fine), an inline
// [a, b] or [{capability: a}] list, and "capability:" in any position of an entry. It never fails: what it
// cannot read it leaves out (relai itself warns about that, `relai index build`).
func parseLists(b []byte) Manifest {
	var m Manifest
	text := strings.ReplaceAll(strings.TrimPrefix(string(b), "\ufeff"), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if l == "" || l[0] == ' ' || l[0] == '\t' || l[0] == '#' || l[0] == '-' {
			continue
		}
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		key := strings.TrimSpace(k)
		if key != "provides" && key != "consumes" && key != "links" {
			continue
		}
		var names []string
		names, i = listAt(lines, i, unquote(stripComment(strings.TrimSpace(v))), strings.TrimSpace(stripComment(strings.TrimSpace(v))))
		switch key {
		case "provides":
			m.Provides = append(m.Provides, names...)
		case "consumes":
			m.Consumes = append(m.Consumes, names...)
		default:
			m.Links = append(m.Links, names...)
		}
	}
	return m
}

// listAt reads the list whose header is lines[start]; rest is the text after the colon.
func listAt(lines []string, start int, _ string, rest string) ([]string, int) {
	var out []string
	if rest != "" {
		if strings.HasPrefix(rest, "[") && strings.HasSuffix(rest, "]") {
			for _, part := range splitTop(rest[1 : len(rest)-1]) {
				part = strings.TrimSpace(part)
				if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
					for _, kv := range splitTop(part[1 : len(part)-1]) {
						if k, v, ok := strings.Cut(kv, ":"); ok && strings.TrimSpace(k) == "capability" {
							out = append(out, unquote(strings.TrimSpace(v)))
						}
					}
				} else if part != "" {
					out = append(out, unquote(part))
				}
			}
		}
		return out, start
	}
	i, gotInItem, inItem := start, false, false
	for i+1 < len(lines) {
		l := lines[i+1]
		raw := strings.TrimSpace(l)
		t := strings.TrimSpace(stripComment(l))
		switch {
		case raw == "" || strings.HasPrefix(raw, "#"):
		case !strings.HasPrefix(l, " ") && !strings.HasPrefix(l, "\t") && !strings.HasPrefix(l, "-"):
			return out, i
		case strings.HasPrefix(t, "- ") || t == "-":
			inItem, gotInItem = true, false
			item := strings.TrimSpace(strings.TrimPrefix(t, "-"))
			if item == "" {
				break
			}
			if k, v, ok := strings.Cut(item, ":"); ok && !strings.HasPrefix(item, "\"") && !strings.HasPrefix(item, "'") {
				if strings.TrimSpace(k) == "capability" {
					out = append(out, unquote(strings.TrimSpace(v)))
					gotInItem = true
				}
			} else {
				out = append(out, unquote(item))
				gotInItem = true
			}
		default:
			if k, v, ok := strings.Cut(t, ":"); ok && inItem && !gotInItem && strings.TrimSpace(k) == "capability" {
				out = append(out, unquote(strings.TrimSpace(v)))
				gotInItem = true
			}
		}
		i++
	}
	return out, i
}

func splitTop(s string) []string {
	var out []string
	depth, start := 0, 0
	var q byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case q != 0:
			if c == q {
				q = 0
			}
		case c == '"' || c == '\'':
			q = c
		case c == '{' || c == '[':
			depth++
		case c == '}' || c == ']':
			depth--
		case c == ',' && depth == 0:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}
