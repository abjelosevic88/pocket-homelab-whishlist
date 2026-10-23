package countries

import "testing"

func TestFind(t *testing.T) {
	for _, c := range []struct {
		text, code string
		want       bool
	}{
		{"Wir liefern nach Deutschland, Österreich und Bosnien und Herzegowina.", "BA", true},
		{"Dostava na području cijele BiH u roku 48h", "BA", true},
		{"We ship to Bosnia & Herzegovina", "BA", true},
		{"Versand nur innerhalb Deutschlands", "BA", false},
		{"ABIHA", "BA", false},
		{"Livraison en Bosnie-Herzégovine", "BA", true},
	} {
		if got, _ := Find(c.text, c.code); (got != "") != c.want {
			t.Errorf("%q: %q", c.text, got)
		}
	}
	if Name("BA") != "Bosnia and Herzegovina" || !Valid("de") || Valid("ZZ") || TLD("GB") != ".uk" {
		t.Fatal("helpers")
	}
	if n := CountIn("Deutschland, Österreich, Frankreich, Italien, Spanien, Belgien", "BA"); n < 5 {
		t.Fatalf("CountIn %d", n)
	}
}
