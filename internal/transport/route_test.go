package transport

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kevindurb/tsgit/internal/authz"
)

func TestParseRoute(t *testing.T) {
	tests := []struct {
		name      string
		method    string
		target    string
		wantName  string
		wantSvc   string
		wantAdv   bool
		wantOp    authz.Op
		wantMake  bool
		wantError bool
	}{
		{
			name: "clone discovery", method: http.MethodGet,
			target:   "/acme/widget.git/info/refs?service=git-upload-pack",
			wantName: "acme/widget", wantSvc: ServiceUploadPack, wantAdv: true, wantOp: authz.OpRead,
		},
		{
			name: "push discovery", method: http.MethodGet,
			target:   "/acme/widget.git/info/refs?service=git-receive-pack",
			wantName: "acme/widget", wantSvc: ServiceReceivePack, wantAdv: true, wantOp: authz.OpWrite, wantMake: true,
		},
		{
			name: "fetch", method: http.MethodPost,
			target:   "/acme/widget.git/git-upload-pack",
			wantName: "acme/widget", wantSvc: ServiceUploadPack, wantOp: authz.OpRead,
		},
		{
			name: "push", method: http.MethodPost,
			target:   "/acme/widget.git/git-receive-pack",
			wantName: "acme/widget", wantSvc: ServiceReceivePack, wantOp: authz.OpWrite, wantMake: true,
		},
		{
			name: "name without suffix", method: http.MethodGet,
			target:   "/acme/widget/info/refs?service=git-upload-pack",
			wantName: "acme/widget", wantSvc: ServiceUploadPack, wantAdv: true, wantOp: authz.OpRead,
		},
		{
			name: "missing service", method: http.MethodGet,
			target: "/acme/widget.git/info/refs", wantError: true,
		},
		{
			name: "unknown service", method: http.MethodGet,
			target: "/acme/widget.git/info/refs?service=git-foo", wantError: true,
		},
		{
			name: "discovery via POST", method: http.MethodPost,
			target: "/acme/widget.git/info/refs?service=git-upload-pack", wantError: true,
		},
		{
			name: "upload via GET", method: http.MethodGet,
			target: "/acme/widget.git/git-upload-pack", wantError: true,
		},
		{
			name: "push via GET", method: http.MethodGet,
			target: "/acme/widget.git/git-receive-pack", wantError: true,
		},
		{
			name: "wrong method", method: http.MethodPut,
			target: "/acme/widget.git/git-upload-pack", wantError: true,
		},
		{
			name: "no repository", method: http.MethodGet,
			target: "/info/refs?service=git-upload-pack", wantError: true,
		},
		{
			name: "one segment", method: http.MethodGet,
			target: "/acme/info/refs?service=git-upload-pack", wantError: true,
		},
		{
			name: "nested namespace", method: http.MethodGet,
			target: "/team/acme/widget.git/info/refs?service=git-upload-pack", wantError: true,
		},
		{
			name: "hidden repository", method: http.MethodGet,
			target: "/acme/.hidden.git/info/refs?service=git-upload-pack", wantError: true,
		},
		{
			name: "traversal", method: http.MethodGet,
			target: "/acme/../../etc.git/info/refs?service=git-upload-pack", wantError: true,
		},
		{
			name: "dotdot segment", method: http.MethodGet,
			target: "/../etc.git/info/refs?service=git-upload-pack", wantError: true,
		},
		{
			name: "unrelated path", method: http.MethodGet,
			target: "/", wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.target, nil)
			got, err := parseRoute(req)
			if tt.wantError {
				if err == nil {
					t.Fatalf("parseRoute = %+v, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseRoute: %v", err)
			}
			if got.name != tt.wantName || got.service != tt.wantSvc || got.advertise != tt.wantAdv ||
				got.op != tt.wantOp || got.create != tt.wantMake {
				t.Fatalf("parseRoute = %+v, want name=%q service=%q advertise=%v op=%q create=%v",
					got, tt.wantName, tt.wantSvc, tt.wantAdv, tt.wantOp, tt.wantMake)
			}
		})
	}
}
