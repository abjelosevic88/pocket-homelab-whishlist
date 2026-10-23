// Package countries knows the world's countries by ISO code and by the names
// shops write them in, so a deal hunt can tell whether a shop delivers to the
// person's country ("Versand nach Bosnien und Herzegowina", "dostava u BiH").
package countries

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// extra are common short names CLDR does not list.
var extra = map[string][]string{
	"BA": {"Bosnia and Herzegovina", "Bosnia-Herzegovina", "BiH", "Bosnien-Herzegowina"},
	"GB": {"United Kingdom", "Great Britain", "UK"},
	"US": {"United States of America", "USA", "U.S."},
	"AE": {"UAE"},
	"CZ": {"Czech Republic"},
	"MK": {"Macedonia", "North Macedonia", "Mazedonien"},
	"RS": {"Srbija"},
	"ME": {"Crna Gora"},
	"XK": {"Kosovo"},
	"NL": {"Holland"},
	"DE": {"Deutschland"},
	"AT": {"Österreich"},
	"CH": {"Schweiz", "Suisse"},
}

// Country is one entry for pickers.
type Country struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// Valid reports whether code is a known country.
func Valid(code string) bool { _, ok := names[strings.ToUpper(code)]; return ok }

// Name is the English name ("" for unknown codes).
func Name(code string) string {
	code = strings.ToUpper(code)
	switch code {
	case "BA":
		return "Bosnia and Herzegovina"
	}
	if n := names[code]; len(n) > 0 {
		return n[0]
	}
	return ""
}

// All lists every country by English name.
func All() []Country {
	out := make([]Country, 0, len(names))
	for c := range names {
		out = append(out, Country{c, Name(c)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Names are every way the country may be written, longest first.
func Names(code string) []string {
	code = strings.ToUpper(code)
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		k := fold(s)
		if len([]rune(k)) >= 3 && !seen[k] {
			seen[k] = true
			out = append(out, s)
		}
	}
	for _, n := range append(append([]string{}, names[code]...), extra[code]...) {
		add(n)
		if strings.Contains(n, "&") {
			for _, and := range []string{"and", "und", "et", "e", "y", "i"} {
				add(strings.ReplaceAll(n, "&", and))
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

// TLD is the country's internet domain (".ba"; ".uk" for GB).
func TLD(code string) string {
	code = strings.ToLower(code)
	if code == "gb" {
		return ".uk"
	}
	return "." + code
}

// fold lowercases and strips accents' spacing quirks for matching.
func fold(s string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(s) {
		if unicode.IsSpace(r) || r == '-' || r == ' ' {
			if !space {
				b.WriteRune(' ')
			}
			space = true
			continue
		}
		space = false
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

// Find returns the first name of the country that text mentions, with the
// text around it ("" when it is not mentioned). Short names ("BiH", "UK")
// must stand as a word; the code itself never counts.
func Find(text, code string) (string, string) {
	low := fold(text)
	for _, n := range Names(code) {
		k := fold(n)
		i := strings.Index(low, k)
		for i >= 0 {
			end := i + len(k)
			prev, _ := utf8.DecodeLastRuneInString(low[:i])
			next, _ := utf8.DecodeRuneInString(low[end:])
			before := i == 0 || !isWord(prev)
			after := end >= len(low) || !isWord(next)
			if before && after {
				from, to := max(0, i-90), min(len(low), end+90)
				return n, strings.TrimSpace(strings.ToValidUTF8(low[from:to], ""))
			}
			j := strings.Index(low[i+1:], k)
			if j < 0 {
				break
			}
			i += 1 + j
		}
	}
	return "", ""
}

func isWord(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// CountIn counts how many countries other than code text names (a shipping
// page that lists ten countries but not yours most likely does not deliver).
func CountIn(text, code string) int {
	low := " " + fold(text) + " "
	n := 0
	for c := range names {
		if c == strings.ToUpper(code) {
			continue
		}
		for _, name := range names[c][:min(3, len(names[c]))] {
			k := fold(name)
			if len(k) >= 5 && strings.Contains(low, " "+k) {
				n++
				break
			}
		}
	}
	return n
}
