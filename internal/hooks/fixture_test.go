package hooks

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestDocumentedHooksFixture keeps docs/access-control.md honest: the example
// cap/hooks payload documented there must parse into real hooks.
func TestDocumentedHooksFixture(t *testing.T) {
	const marker = "<!-- webhooks-capability-fixture -->"
	data, err := os.ReadFile("../../docs/access-control.md")
	if err != nil {
		t.Fatalf("read access-control doc: %v", err)
	}
	text := string(data)
	idx := strings.Index(text, marker)
	if idx < 0 {
		t.Fatalf("access-control doc is missing %s", marker)
	}
	rest := text[idx+len(marker):]
	start := strings.Index(rest, "```json")
	if start < 0 {
		t.Fatal("webhooks fixture is not a json code block")
	}
	rest = rest[start+len("```json"):]
	end := strings.Index(rest, "```")
	if end < 0 {
		t.Fatal("webhooks fixture code block is not closed")
	}
	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(rest[:end]), &raw); err != nil {
		t.Fatalf("documented fixture does not parse: %v", err)
	}
	if len(raw) != 2 {
		t.Fatalf("documented fixture has %d entries, want 2", len(raw))
	}
	var hooks []Hook
	for i, entry := range raw {
		h, err := parseHook(string(entry))
		if err != nil {
			t.Fatalf("documented fixture entry %d does not parse: %v", i, err)
		}
		hooks = append(hooks, h)
	}
	for _, h := range hooks {
		if !h.Matches("acme/widget") {
			t.Errorf("documented fixture hook for %q does not match acme/widget", h.URL)
		}
	}
	if hooks[0].SecretEnv != "" {
		t.Errorf("unsigned fixture entry has secretEnv %q", hooks[0].SecretEnv)
	}
	if hooks[1].SecretEnv != "TSGIT_HOOK_BACKUP_CI" {
		t.Errorf("signed fixture entry secretEnv = %q", hooks[1].SecretEnv)
	}
}
