package aggregate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestConformance runs every relai.yaml of testdata/conformance (a copy of RELAI's spec/conformance) through
// this reader and compares what it extracts with what the case declares. This reader extracts only id,
// summary, provides, consumes and links. The other keys a case declares (apiVersion, owners, conventions,
// tasks) are not read here, so for them the case only checks that the file is accepted: that concerns
// 02-full-block (owners, conventions, tasks), 03-inline-lists (owners) and every case with apiVersion.
// 05-unknown-keys checks that unknown keys are ignored without an error.
func TestConformance(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("testdata", "conformance", "*.yaml"))
	if len(files) == 0 {
		t.Fatal("no conformance cases found")
	}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".yaml")
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(strings.TrimSuffix(f, ".yaml") + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var want map[string]any
			if err := json.Unmarshal(raw, &want); err != nil {
				t.Fatal(err)
			}
			id, summary, err := parseManifest(src)
			if err != nil {
				t.Fatalf("refused: %v", err)
			}
			m := parseLists(src)
			str := func(k string) string { s, _ := want[k].(string); return s }
			list := func(k string) []string {
				var out []string
				if a, ok := want[k].([]any); ok {
					for _, v := range a {
						out = append(out, v.(string))
					}
				}
				return out
			}
			if id != str("id") || summary != str("summary") {
				t.Errorf("id/summary = %q/%q, want %q/%q", id, summary, str("id"), str("summary"))
			}
			for k, got := range map[string][]string{"provides": m.Provides, "consumes": m.Consumes, "links": m.Links} {
				if w := list(k); !reflect.DeepEqual(got, w) && (len(got) != 0 || len(w) != 0) {
					t.Errorf("%s = %v, want %v", k, got, w)
				}
			}
		})
	}
}

func TestUnknownAPIVersionIsRefused(t *testing.T) {
	for _, v := range []string{"relai.dev/v1", "\"relai.dev/v2\"", "nope"} {
		if _, _, err := parseManifest([]byte("apiVersion: " + v + "\nid: x\nsummary: s\n")); err == nil {
			t.Errorf("apiVersion %s accepted", v)
		}
	}
	for _, src := range []string{"id: x\n", "apiVersion: relai.dev/v0 # ok\r\nid: x\r\n", "\ufeffapiVersion: 'relai.dev/v0'\nid: x\n"} {
		if id, _, err := parseManifest([]byte(src)); err != nil || id != "x" {
			t.Errorf("%q: id=%q err=%v", src, id, err)
		}
	}
}

func TestRefusedManifestSkipsTheRepo(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "relai.yaml"), []byte("apiVersion: relai.dev/v9\nid: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &Aggregator{Run: func(string, string, ...string) ([]byte, error) { return nil, nil }}
	if _, err := a.read(dir); err == nil || !strings.Contains(err.Error(), "relai.dev/v9") {
		t.Fatalf("err = %v, want a refusal naming the version", err)
	}
}
