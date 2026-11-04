package aggregate

import (
	"reflect"
	"testing"
)

func TestListsAreReadInEveryShape(t *testing.T) {
	want := []string{"auth.oidc", "auth.session"}
	for name, src := range map[string]string{
		"block":        "id: x\nprovides:\n  - capability: auth.oidc\n  - capability: auth.session\n",
		"inline maps":  "id: x\nprovides: [{capability: auth.oidc}, {capability: auth.session}]\n",
		"inline":       "id: x\nprovides: [auth.oidc, \"auth.session\"]\n",
		"scalar":       "id: x\nprovides:\n  - auth.oidc\n  - auth.session\n",
		"comments":     "id: x\nprovides:\n  # one\n  - capability: auth.oidc\n\n  - description: d\n    capability: auth.session\nsummary: s\n",
		"crlf and bom": "\ufeffid: x\r\nprovides:\r\n  - auth.oidc\r\n  - auth.session\r\n",
	} {
		if got := parseLists([]byte(src)).Provides; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: provides = %v, want %v", name, got, want)
		}
	}
	m := parseLists([]byte("id: x\nconsumes:\n  - capability: auth.oidc\nlinks: [portfolio, shelfmark]\n"))
	if !reflect.DeepEqual(m.Consumes, []string{"auth.oidc"}) || !reflect.DeepEqual(m.Links, []string{"portfolio", "shelfmark"}) || len(m.Provides) != 0 {
		t.Errorf("consumes/links = %+v", m)
	}
}
