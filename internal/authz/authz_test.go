package authz

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestDecide(t *testing.T) {
	write := []Grant{{Repos: []string{"acme/*"}, Access: AccessWrite}}
	read := []Grant{{Repos: []string{"acme/*"}, Access: AccessRead}}
	admin := []Grant{{Repos: []string{"acme/*"}, Access: AccessAdmin}}
	both := []Grant{
		{Repos: []string{"acme/*"}, Access: AccessRead},
		{Repos: []string{"acme/widget"}, Access: AccessWrite},
	}
	unionWithAdmin := []Grant{
		{Repos: []string{"acme/widget"}, Access: AccessWrite},
		{Repos: []string{"acme/*"}, Access: AccessAdmin},
	}

	tests := []struct {
		name      string
		grants    []Grant
		repo      string
		op        Op
		allow     bool
		canMake   bool
		canDelete bool
	}{
		{name: "no grants deny read", repo: "acme/widget", op: OpRead, allow: false},
		{name: "no grants deny write", repo: "acme/widget", op: OpWrite, allow: false},
		{name: "no grants deny create", repo: "acme/widget", op: OpCreate, allow: false},
		{name: "no grants deny delete", repo: "acme/widget", op: OpDelete, allow: false},
		{name: "read allows read", grants: read, repo: "acme/widget", op: OpRead, allow: true},
		{name: "read denies write", grants: read, repo: "acme/widget", op: OpWrite, allow: false},
		{name: "read denies create", grants: read, repo: "acme/widget", op: OpCreate, allow: false},
		{name: "read denies delete", grants: read, repo: "acme/widget", op: OpDelete, allow: false},
		{name: "write allows read", grants: write, repo: "acme/widget", op: OpRead, allow: true, canMake: true},
		{name: "write allows write", grants: write, repo: "acme/widget", op: OpWrite, allow: true, canMake: true},
		{name: "write allows create", grants: write, repo: "acme/widget", op: OpCreate, allow: true, canMake: true},
		{name: "write denies delete", grants: write, repo: "acme/widget", op: OpDelete, allow: false, canMake: true},
		{name: "write does not cross namespace", grants: write, repo: "other/widget", op: OpWrite, allow: false},
		{name: "admin allows read", grants: admin, repo: "acme/widget", op: OpRead, allow: true, canMake: true, canDelete: true},
		{name: "admin allows write", grants: admin, repo: "acme/widget", op: OpWrite, allow: true, canMake: true, canDelete: true},
		{name: "admin allows create", grants: admin, repo: "acme/widget", op: OpCreate, allow: true, canMake: true, canDelete: true},
		{name: "admin allows delete", grants: admin, repo: "acme/widget", op: OpDelete, allow: true, canMake: true, canDelete: true},
		{name: "admin does not cross namespace", grants: admin, repo: "other/widget", op: OpDelete, allow: false},
		{name: "admin does not cross namespace for read", grants: admin, repo: "other/widget", op: OpRead, allow: false},
		{name: "union takes highest level", grants: both, repo: "acme/widget", op: OpWrite, allow: true, canMake: true},
		{name: "union with admin takes admin", grants: unionWithAdmin, repo: "acme/widget", op: OpDelete, allow: true, canMake: true, canDelete: true},
		{name: "read pattern does not match another repo", grants: read, repo: "shared/docs", op: OpRead, allow: false},
		{name: "star does not cross separator", grants: []Grant{{Repos: []string{"acme*"}, Access: AccessWrite}}, repo: "acme/widget", op: OpRead, allow: false},
		{name: "malformed pattern grants nothing", grants: []Grant{{Repos: []string{"["}, Access: AccessWrite}}, repo: "acme/widget", op: OpRead, allow: false},
		{name: "tagged identity needs no login", grants: write, repo: "acme/widget", op: OpRead, allow: true, canMake: true},
		{name: "unknown operation denies", grants: write, repo: "acme/widget", op: Op("archive"), allow: false, canMake: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := Identity{Node: "node", Grants: tt.grants}
			if tt.name == "tagged identity needs no login" {
				id.Tags = []string{"tag:ci"}
			}
			d := Decide(id, tt.repo, tt.op)
			if d.Allow != tt.allow {
				t.Fatalf("Allow = %v, want %v (reason %q)", d.Allow, tt.allow, d.Reason)
			}
			if d.CanCreate != tt.canMake {
				t.Fatalf("CanCreate = %v, want %v", d.CanCreate, tt.canMake)
			}
			if d.CanDelete != tt.canDelete {
				t.Fatalf("CanDelete = %v, want %v", d.CanDelete, tt.canDelete)
			}
		})
	}
}

func TestAccessRejectsUnknownValue(t *testing.T) {
	var g Grant
	err := json.Unmarshal([]byte(`{"repos":["acme/*"],"access":"owner"}`), &g)
	if err == nil {
		t.Fatal("unknown access value must fail decoding")
	}
}

func TestAccessParsesDocumentedValues(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Access
	}{
		{`{"repos":["a/b"],"access":"read"}`, AccessRead},
		{`{"repos":["a/b"],"access":"write"}`, AccessWrite},
		{`{"repos":["a/b"],"access":"admin"}`, AccessAdmin},
	} {
		var g Grant
		if err := json.Unmarshal([]byte(tc.in), &g); err != nil {
			t.Fatalf("decode %s: %v", tc.in, err)
		}
		if g.Access != tc.want {
			t.Fatalf("access = %v, want %v", g.Access, tc.want)
		}
	}
}

// TestDocumentedCapabilityFixture keeps docs/access-control.md honest: the
// example payload must parse into real grants.
func TestDocumentedCapabilityFixture(t *testing.T) {
	const marker = "<!-- capability-fixture -->"
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
		t.Fatal("capability fixture is not a json code block")
	}
	rest = rest[start+len("```json"):]
	end := strings.Index(rest, "```")
	if end < 0 {
		t.Fatal("capability fixture code block is not closed")
	}
	var grants []Grant
	if err := json.Unmarshal([]byte(rest[:end]), &grants); err != nil {
		t.Fatalf("documented fixture does not parse: %v", err)
	}
	if len(grants) != 3 {
		t.Fatalf("documented fixture has %d grants, want 3", len(grants))
	}
	id := Identity{Grants: grants}
	if got := Decide(id, "acme/widget", OpWrite); !got.Allow {
		t.Fatal("documented write grant does not allow acme/widget")
	}
	if got := Decide(id, "acme/admin", OpDelete); !got.Allow || !got.CanDelete {
		t.Fatal("documented admin grant does not allow deleting acme/admin")
	}
	if got := Decide(id, "acme/widget", OpDelete); got.Allow {
		t.Fatal("documented write grant unexpectedly allows deletion")
	}
	if got := Decide(id, "shared/docs", OpRead); !got.Allow {
		t.Fatal("documented read grant does not allow shared/docs")
	}
	if got := Decide(id, "other/widget", OpWrite); got.Allow {
		t.Fatal("documented grant unexpectedly allows other/widget")
	}
}

func TestPublicIdentityIsReadOnly(t *testing.T) {
	id := PublicIdentity([]FunnelGrant{
		{Repos: []string{"public/*", "shared/docs"}},
	})
	if id.Node != FunnelNode {
		t.Fatalf("public node = %q, want %q", id.Node, FunnelNode)
	}
	for _, repo := range []string{"public/widget", "shared/docs"} {
		if got := Decide(id, repo, OpRead); !got.Allow {
			t.Fatalf("public read of %s denied: %+v", repo, got)
		}
	}
	for _, repo := range []string{"public/widget", "shared/docs", "other/secret"} {
		for _, op := range []Op{OpWrite, OpCreate, OpDelete} {
			got := Decide(id, repo, op)
			if got.Allow || got.CanCreate || got.CanDelete {
				t.Fatalf("public %s of %s allowed: %+v", op, repo, got)
			}
		}
	}
}

func TestFunnelGrantRejectsAccessAndUnknownFields(t *testing.T) {
	for _, payload := range []string{
		`{"repos":["public/*"],"access":"write"}`,
		`{"repos":["public/*"],"access":"read"}`,
		`{"repos":["public/*"],"extra":true}`,
		`["public/*"]`,
	} {
		var g FunnelGrant
		if err := json.Unmarshal([]byte(payload), &g); err == nil {
			t.Errorf("FunnelGrant accepted %s: %+v", payload, g)
		}
	}
}

func TestFunnelGrantKeepsOnlyRepos(t *testing.T) {
	var g FunnelGrant
	if err := json.Unmarshal([]byte(`{"repos":["public/*","shared/docs"]}`), &g); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(g.Repos) != 2 || g.Repos[0] != "public/*" || g.Repos[1] != "shared/docs" {
		t.Fatalf("repos = %v", g.Repos)
	}
}
