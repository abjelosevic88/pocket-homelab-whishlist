package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/config"
	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/store"
)

// moveLinks starts Wishlist behind a fake Core whose pcx_<who>_<export|import|read>
// tokens are Pocket Move links acting for alice@ or bob@ (plain members to Core).
func moveLinks(t *testing.T) *app {
	t.Helper()
	coreSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/auth/introspect" {
			io.WriteString(w, `{}`)
			return
		}
		var in struct {
			Token string `json:"token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		parts := strings.SplitN(strings.TrimPrefix(in.Token, "pcx_"), "_", 2)
		if len(parts) != 2 {
			io.WriteString(w, `{"active":false}`)
			return
		}
		scope := map[string]string{"export": core.ScopeMoveExport, "import": core.ScopeMoveImport, "read": "wishes:read"}[parts[1]]
		email := parts[0] + "@example.com"
		fmt.Fprintf(w, `{"active":true,"kind":"delegated","scope":%q,"audience":"wishlist","user":{"id":1,"sub":%q,"email":%q,"name":%q,"role":"member"},"app":{"id":7,"slug":"move","name":"Pocket Move"}}`, scope, email, email, parts[0])
	}))
	t.Cleanup(coreSrv.Close)
	return newApp(t, func(c *config.Config) { c.CoreURL = coreSrv.URL })
}

func (a *app) raw(tok, method, path, ctype string, body []byte) (int, []byte) {
	a.t.Helper()
	req, _ := http.NewRequest(method, a.url+path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

func TestMoveLinkScopes(t *testing.T) {
	a := moveLinks(t)
	exp, imp, read := "pcx_bob_export", "pcx_bob_import", "pcx_bob_read"
	for _, c := range []struct {
		tok, method, path string
		want              int
	}{
		{exp, "GET", core.PortablePath, 200},
		{exp, "GET", "/api/v1/export.json", 200},
		{exp, "POST", "/api/v1/import", 403},
		{exp, "GET", "/api/v1/settings", 403},
		{exp, "GET", "/api/v1/lists", 403},
		{exp, "GET", "/api/v1/backup", 403},
		{exp, "GET", "/api/v1/users", 403},
		{imp, "GET", core.PortablePath, 200},
		{imp, "GET", "/api/v1/export.json", 403},
		{imp, "PUT", "/api/v1/settings", 403},
		{imp, "POST", "/api/v1/lists", 403},
		{imp, "POST", "/api/v1/wishes", 403},
		{read, "GET", core.PortablePath, 403},
		{read, "GET", "/api/v1/export.json", 403},
		{read, "POST", "/api/v1/import", 403},
	} {
		if code, body := a.raw(c.tok, c.method, c.path, "application/json", []byte("{}")); code != c.want {
			t.Errorf("%s %s %s → %d, want %d (%s)", c.tok, c.method, c.path, code, c.want, body)
		}
	}
	_, body := a.raw(exp, "GET", core.PortablePath, "", nil)
	var p core.Portable
	if err := json.Unmarshal(body, &p); err != nil || len(p.Exports) != 1 || len(p.Imports) != 1 || p.Exports[0].Kind != core.KindPocket {
		t.Fatalf("portable: %s", body)
	}
}

// Move brings Alice's lists to Bob: the import adds once (archived lists
// too), and nothing of one person shows up for the other.
func TestMoveImportsAddOnce(t *testing.T) {
	a := moveLinks(t)
	l := a.ok(a.alice, "POST", "/api/v1/lists", map[string]any{"name": "Birthday"})
	a.ok(a.alice, "POST", "/api/v1/wishes", map[string]any{"list_id": num(l, "id"), "title": "Hiking boots", "price": 12000})
	old := a.ok(a.alice, "POST", "/api/v1/lists", map[string]any{"name": "Old flat"})
	a.ok(a.alice, "POST", "/api/v1/wishes", map[string]any{"list_id": num(old, "id"), "title": "Sofa", "price": 50000})
	a.ok(a.alice, "PUT", fmt.Sprintf("/api/v1/lists/%d", num(old, "id")), map[string]any{"archived": true})

	code, export := a.raw("pcx_alice_export", "GET", "/api/v1/export.json", "", nil)
	if code != 200 || !strings.Contains(string(export), "Hiking boots") || !strings.Contains(string(export), "Sofa") {
		t.Fatalf("alice's export: %d %s", code, export)
	}
	if _, bob := a.raw("pcx_bob_export", "GET", "/api/v1/export.json", "", nil); strings.Contains(string(bob), "Hiking boots") {
		t.Fatalf("bob's export holds alice's wishes: %s", bob)
	}

	imp := func() map[string]any {
		code, out := a.raw("pcx_bob_import", "POST", "/api/v1/import", "application/json", export)
		var m map[string]any
		if code != 200 || json.Unmarshal(out, &m) != nil {
			t.Fatalf("import: %d %s", code, out)
		}
		return m
	}
	if first := imp(); num(first, "lists") != 2 || num(first, "wishes") != 2 {
		t.Fatalf("first import: %v", first)
	}
	if second := imp(); num(second, "lists") != 0 || num(second, "wishes") != 0 {
		t.Fatalf("second import added: %v", second)
	}

	ctx := t.Context()
	count := func(uid int64) (lists, wishes int) {
		for _, arch := range []bool{false, true} {
			ls, _ := a.st.ListLists(ctx, uid, arch)
			for _, l := range ls {
				ws, _ := a.st.ListWishes(ctx, uid, store.WishFilter{Status: "all", ListID: l.ID})
				lists, wishes = lists+1, wishes+len(ws)
			}
		}
		return
	}
	if l, w := count(a.ids["Bob"]); l != 2 || w != 2 {
		t.Fatalf("bob has %d lists, %d wishes", l, w)
	}
	if l, w := count(a.ids["Alice"]); l != 2 || w != 2 {
		t.Fatalf("alice has %d lists, %d wishes", l, w)
	}
}
