package web

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/kevindurb/tsgit/internal/repos"
)

const (
	mainHash    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	featureHash = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	tagHash     = "cccccccccccccccccccccccccccccccccccccccc"
)

func browseCommit(hash, ref string) repos.Commit {
	return repos.Commit{
		Hash:    hash,
		Ref:     ref,
		Subject: "initial commit",
		When:    time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC),
	}
}

func browseStore() *fakeStore {
	store := &fakeStore{
		infos: []repos.Info{{
			Name:     "acme/widget",
			Branch:   "main",
			Branches: 2,
			Tags:     1,
			Refs:     []string{"main", "feature", "v1"},
			Latest:   browseCommit(mainHash, ""),
		}},
	}
	store.onResolveRef = func(_, ref string) (repos.Commit, error) {
		switch ref {
		case "", "HEAD":
			return browseCommit(mainHash, "HEAD"), nil
		case "main", "refs/heads/main", mainHash, strings.ToUpper(mainHash):
			return browseCommit(mainHash, "refs/heads/main"), nil
		case "feature", "refs/heads/feature", featureHash:
			return browseCommit(featureHash, "refs/heads/feature"), nil
		case "v1", "refs/tags/v1", tagHash:
			return browseCommit(tagHash, "refs/tags/v1"), nil
		default:
			return repos.Commit{}, repos.ErrNotExist
		}
	}
	store.onTree = func(_, _, path string) ([]repos.Entry, error) {
		switch path {
		case "":
			return []repos.Entry{
				{Name: "docs", Mode: "040000", Type: "tree", Hash: "d", Dir: true},
				{Name: "README.md", Mode: "100644", Type: "blob", Hash: "r", Size: 20},
				{Name: "a b.txt", Mode: "100644", Type: "blob", Hash: "a", Size: 6},
				{Name: "bin.dat", Mode: "100644", Type: "blob", Hash: "b", Size: 12},
				{Name: "empty.txt", Mode: "100644", Type: "blob", Hash: "e", Size: 0},
				{Name: "large.bin", Mode: "100644", Type: "blob", Hash: "l", Size: repos.MaxBlobBytes + 1},
				{Name: "link", Mode: "120000", Type: "blob", Hash: "s", Size: 6, Symlink: true},
				{Name: "markup.html", Mode: "100644", Type: "blob", Hash: "m", Size: 25},
				{Name: "run.sh", Mode: "100755", Type: "blob", Hash: "x", Size: 12, Executable: true},
				{Name: "sub", Mode: "160000", Type: "commit", Hash: "deadbeef", Size: -1, Submodule: true},
				{Name: "café.txt", Mode: "100644", Type: "blob", Hash: "u", Size: 8},
			}, nil
		case "docs":
			return []repos.Entry{
				{Name: "guide.md", Mode: "100644", Type: "blob", Hash: "g", Size: 10},
			}, nil
		case "a b.txt", "README.md", "missing":
			if path == "missing" {
				return nil, repos.ErrNotExist
			}
			return nil, repos.ErrNotTree
		default:
			return nil, repos.ErrNotExist
		}
	}
	store.onBlob = func(_, _, path string) (repos.Blob, error) {
		switch path {
		case "README.md":
			return repos.Blob{Path: path, Mode: "100644", Size: 20, Data: []byte("# Widget\n\nSee [guide](docs/guide.md).\n")}, nil
		case "a b.txt":
			return repos.Blob{Path: path, Mode: "100644", Size: 6, Data: []byte("hello\n")}, nil
		case "markup.html":
			return repos.Blob{Path: path, Mode: "100644", Size: 25, Data: []byte("<script>alert(1)</script>")}, nil
		case "bin.dat":
			return repos.Blob{Path: path, Mode: "100644", Size: 12, Binary: true}, nil
		case "large.bin":
			return repos.Blob{Path: path, Mode: "100644", Size: repos.MaxBlobBytes + 1, TooLarge: true}, nil
		case "empty.txt":
			return repos.Blob{Path: path, Mode: "100644", Size: 0, Empty: true}, nil
		case "link":
			return repos.Blob{Path: path, Mode: "120000", Size: 6, Symlink: true, LinkTarget: "a b.txt"}, nil
		case "sub":
			return repos.Blob{Path: path, Mode: "160000", Size: -1, Submodule: true, Hash: "deadbeef"}, nil
		case "docs":
			return repos.Blob{}, repos.ErrNotBlob
		default:
			return repos.Blob{}, repos.ErrNotExist
		}
	}
	store.onReadme = func(_, commit string) (repos.Blob, bool, error) {
		switch commit {
		case mainHash:
			return repos.Blob{Path: "README.md", Mode: "100644", Data: []byte("# Widget\n\nSee [guide](docs/guide.md).\n")}, true, nil
		case featureHash:
			return repos.Blob{Path: "README.md", Mode: "100644", Data: []byte("# Feature\n")}, true, nil
		default:
			return repos.Blob{}, false, nil
		}
	}
	return store
}

func TestBrowsingTreeRoute(t *testing.T) {
	rec := request(newTestHandler(t, readerIdentity(), browseStore()), "GET", "/acme/widget/tree")
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"acme/widget",
		`href="/acme/widget/tree/docs"`,
		`href="/acme/widget/blob/a%20b.txt"`,
		`href="/acme/widget/blob/caf%C3%A9.txt"`,
		"README.md",
		"directory",
		"executable",
		"submodule",
		"symlink",
		"1.0 MiB",
		`class="ref-switcher"`,
		`id="ref"`,
		`href="/acme/widget/blob/docs/guide.md"`,
		`id="widget"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("tree page does not contain %q:\n%s", want, body)
		}
	}
	for _, unwanted := range []string{"<script", `<link rel="stylesheet" href="http`, "<img", "@import", "@font-face"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("tree page contains external or active content %q", unwanted)
		}
	}
}

func TestBrowsingNestedTreePreservesRef(t *testing.T) {
	body := request(newTestHandler(t, readerIdentity(), browseStore()), "GET", "/acme/widget/tree/docs?ref=feature").Body.String()
	for _, want := range []string{
		"guide.md",
		`href="/acme/widget/blob/docs/guide.md?ref=feature"`,
		`href="/acme/widget/tree?ref=feature"`,
		`href="/acme/widget/tree/docs?ref=feature"`,
		`value="feature" selected`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("nested tree page does not contain %q:\n%s", want, body)
		}
	}
}

func TestBrowsingTagRefCarriesFullTag(t *testing.T) {
	body := request(newTestHandler(t, readerIdentity(), browseStore()), "GET", "/acme/widget/tree?ref=v1").Body.String()
	if !strings.Contains(body, `href="/acme/widget/blob/README.md?ref=refs%2Ftags%2Fv1"`) {
		t.Fatalf("tag ref links are not canonical:\n%s", body)
	}
}

func TestBrowsingBlobTextAndMarkup(t *testing.T) {
	body := request(newTestHandler(t, readerIdentity(), browseStore()), "GET", "/acme/widget/blob/a%20b.txt").Body.String()
	if !strings.Contains(body, "hello") || !strings.Contains(body, `href="/acme/widget/tree"`) {
		t.Fatalf("text blob page is wrong:\n%s", body)
	}

	body = request(newTestHandler(t, readerIdentity(), browseStore()), "GET", "/acme/widget/blob/markup.html").Body.String()
	if !strings.Contains(body, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatalf("markup was not escaped:\n%s", body)
	}
	if strings.Contains(body, "<script>alert") {
		t.Fatal("repository markup was rendered as active HTML")
	}
}

func TestBrowsingBlobKinds(t *testing.T) {
	cases := map[string]string{
		"bin.dat":   "binary and is not displayed",
		"large.bin": "too large to display",
		"link":      "Symbolic link to",
		"sub":       "Submodule at commit",
	}
	for path, want := range cases {
		body := request(newTestHandler(t, readerIdentity(), browseStore()), "GET", "/acme/widget/blob/"+path).Body.String()
		if !strings.Contains(body, want) {
			t.Errorf("blob page for %s does not contain %q:\n%s", path, want, body)
		}
	}

	body := request(newTestHandler(t, readerIdentity(), browseStore()), "GET", "/acme/widget/blob/empty.txt").Body.String()
	if !strings.Contains(body, `class="blob empty-blob"`) {
		t.Fatalf("empty blob did not render an empty view:\n%s", body)
	}
}

func TestBrowsingNounMismatchRedirects(t *testing.T) {
	rec := request(newTestHandler(t, readerIdentity(), browseStore()), "GET", "/acme/widget/tree/a%20b.txt")
	if rec.Code != 302 || rec.Header().Get("Location") != "/acme/widget/blob/a%20b.txt" {
		t.Fatalf("tree-on-file redirect = %d, %q", rec.Code, rec.Header().Get("Location"))
	}

	rec = request(newTestHandler(t, readerIdentity(), browseStore()), "GET", "/acme/widget/blob/docs")
	if rec.Code != 302 || rec.Header().Get("Location") != "/acme/widget/tree/docs" {
		t.Fatalf("blob-on-directory redirect = %d, %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestBrowsingDotGitRedirectPreservesTailAndQuery(t *testing.T) {
	rec := request(newTestHandler(t, readerIdentity(), browseStore()), "GET", "/acme/widget.git/tree/docs?ref=feature")
	if rec.Code != 301 {
		t.Fatalf("status = %d, want 301", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/acme/widget/tree/docs?ref=feature" {
		t.Fatalf("Location = %q", got)
	}
}

func TestBrowsingSummaryRefRedirectsToTree(t *testing.T) {
	rec := request(newTestHandler(t, readerIdentity(), browseStore()), "GET", "/acme/widget?ref=feature")
	if rec.Code != 302 || rec.Header().Get("Location") != "/acme/widget/tree?ref=feature" {
		t.Fatalf("summary ref redirect = %d, %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestBrowsingRejectsInvalidPaths(t *testing.T) {
	handler := newTestHandler(t, readerIdentity(), browseStore())
	for _, target := range []string{
		"/acme/widget/tree/../secret",
		"/acme/widget/tree/a//b",
		"/acme/widget/tree/.",
		"/acme/widget/blob/",
		"/acme/widget/blob/..",
	} {
		if rec := request(handler, "GET", target); rec.Code != 404 {
			t.Errorf("GET %s = %d, want 404", target, rec.Code)
		}
	}
}

func TestBrowsingRefusesMutation(t *testing.T) {
	handler := newTestHandler(t, readerIdentity(), browseStore())
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		for _, target := range []string{"/acme/widget/tree", "/acme/widget/blob/a%20b.txt"} {
			rec := request(handler, method, target)
			if rec.Code != 405 {
				t.Errorf("%s %s = %d, want 405", method, target, rec.Code)
			}
		}
	}
}

func TestBrowsingIdentityFailure(t *testing.T) {
	store := browseStore()
	called := false
	store.onResolveRef = func(string, string) (repos.Commit, error) {
		called = true
		return repos.Commit{}, nil
	}
	handler := New(Config{
		Resolver: fakeResolver{err: errors.New("no whois")},
		Store:    store,
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	rec := request(handler, "GET", "/acme/widget/tree")
	if rec.Code != 503 {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if called {
		t.Fatal("store was read after identity failure")
	}
}

func TestBrowsingInvalidRefIsNotFound(t *testing.T) {
	rec := request(newTestHandler(t, readerIdentity(), browseStore()), "GET", "/acme/widget/tree?ref=no-such-ref")
	if rec.Code != 404 {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestBrowsingReadTimeoutIsRetryable(t *testing.T) {
	store := browseStore()
	store.onResolveRef = func(string, string) (repos.Commit, error) {
		return repos.Commit{}, context.DeadlineExceeded
	}
	rec := request(newTestHandler(t, readerIdentity(), store), "GET", "/acme/widget/tree")
	if rec.Code != 503 {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("timeout response has no Retry-After")
	}
}

func TestBrowsingDeniedAndMissingIdentical(t *testing.T) {
	handler := newTestHandler(t, readerIdentity(), browseStore())
	denied := request(handler, "GET", "/other/secret/tree")
	missing := request(handler, "GET", "/acme/missing/tree")
	if denied.Code != 404 || missing.Code != 404 {
		t.Fatalf("statuses = %d/%d, want 404/404", denied.Code, missing.Code)
	}
	if denied.Body.String() != missing.Body.String() {
		t.Fatal("denied and missing browsing responses differ")
	}
}

func TestSummaryRendersReadmeAtHead(t *testing.T) {
	store := browseStore()
	store.infos[0].Empty = false
	store.infos[0].Latest = browseCommit(featureHash, "")
	body := request(newTestHandler(t, readerIdentity(), store), "GET", "/acme/widget").Body.String()
	if !strings.Contains(body, "See") || !strings.Contains(body, `href="/acme/widget/blob/docs/guide.md"`) {
		t.Fatalf("summary did not render the HEAD README:\n%s", body)
	}
	if strings.Contains(body, "# Feature") {
		t.Fatal("summary used a non-HEAD README")
	}
}

func TestSummaryReadmeMissingDegrades(t *testing.T) {
	store := browseStore()
	store.onReadme = func(string, string) (repos.Blob, bool, error) {
		return repos.Blob{}, false, nil
	}
	body := request(newTestHandler(t, readerIdentity(), store), "GET", "/acme/widget").Body.String()
	if strings.Contains(body, `class="readme`) {
		t.Fatal("summary rendered a README section when none exists")
	}
	if !strings.Contains(body, "widget service") && !strings.Contains(body, "acme/widget") {
		t.Fatal("summary metadata disappeared")
	}
}

func TestSummaryReadmeFailureDegrades(t *testing.T) {
	store := browseStore()
	store.onReadme = func(string, string) (repos.Blob, bool, error) {
		return repos.Blob{}, false, errors.New("boom")
	}
	body := request(newTestHandler(t, readerIdentity(), store), "GET", "/acme/widget").Body.String()
	if !strings.Contains(body, "README is unavailable") {
		t.Fatalf("summary did not report the README failure:\n%s", body)
	}
	if !strings.Contains(body, "acme/widget") {
		t.Fatal("summary metadata disappeared after a README failure")
	}
}

func TestTreeReadmeFailureDegrades(t *testing.T) {
	store := browseStore()
	store.onReadme = func(string, string) (repos.Blob, bool, error) {
		return repos.Blob{}, false, errors.New("boom")
	}
	body := request(newTestHandler(t, readerIdentity(), store), "GET", "/acme/widget/tree").Body.String()
	if !strings.Contains(body, "README is unavailable") {
		t.Fatalf("tree page did not report the README failure:\n%s", body)
	}
	if !strings.Contains(body, "README.md") || !strings.Contains(body, "directory") {
		t.Fatalf("tree listing disappeared after a README failure:\n%s", body)
	}
}

func TestPublicOversizedReadmeIsNotRendered(t *testing.T) {
	store := browseStore()
	store.infos[0].Name = "public/widget"
	store.onReadme = func(string, string) (repos.Blob, bool, error) {
		return repos.Blob{Path: "README.md", Mode: "100644", Size: repos.MaxReadmeBytes + 1, TooLarge: true}, true, nil
	}
	body := request(newPublicTestHandler(t, store), "GET", "/public/widget/tree").Body.String()
	if !strings.Contains(body, "README is too large to render") {
		t.Fatalf("public oversized README did not report the limit:\n%s", body)
	}
	if strings.Contains(body, `class="readme`) {
		t.Fatal("public oversized README was rendered")
	}
}

func TestPublicBrowsingHidesWriteAffordances(t *testing.T) {
	store := browseStore()
	store.infos[0].Name = "public/widget"
	store.infos[0].Branch = "main"
	store.onReadme = func(string, string) (repos.Blob, bool, error) {
		return repos.Blob{
			Path: "README.md",
			Mode: "100644",
			Data: []byte("[delete](/ .tsgit/delete/public/widget)\n\n[relative](README.md)\n"),
		}, true, nil
	}
	body := request(newPublicTestHandler(t, store), "GET", "/public/widget/tree").Body.String()
	for _, unwanted := range []string{"badge-write", "badge-admin", "Delete", "git push", "/.tsgit/delete"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("public browsing page contains write affordance %q:\n%s", unwanted, body)
		}
	}
	if !strings.Contains(body, `href="/public/widget/blob/README.md"`) {
		t.Fatalf("public README relative link missing:\n%s", body)
	}
}

func TestIconFor(t *testing.T) {
	cases := []struct {
		name  string
		entry repos.Entry
		want  string
	}{
		{"dir", repos.Entry{Name: "docs", Dir: true}, "folder"},
		{"submodule", repos.Entry{Name: "sub", Submodule: true}, "submodule"},
		{"symlink", repos.Entry{Name: "link", Symlink: true}, "symlink"},
		{"executable", repos.Entry{Name: "run.sh", Executable: true}, "executable"},
		{"executable beats extension", repos.Entry{Name: "deploy.py", Executable: true}, "executable"},
		{"doc markdown", repos.Entry{Name: "README.md"}, "doc"},
		{"doc adoc", repos.Entry{Name: "GUIDE.ADOC"}, "doc"},
		{"doc txt", repos.Entry{Name: "notes.txt"}, "doc"},
		{"code go", repos.Entry{Name: "main.go"}, "code"},
		{"code ts", repos.Entry{Name: "app.tsx"}, "code"},
		{"code header", repos.Entry{Name: "thing.hpp"}, "code"},
		{"code shell", repos.Entry{Name: "build.sh"}, "code"},
		{"image png", repos.Entry{Name: "logo.PNG"}, "image"},
		{"image svg", repos.Entry{Name: "diagram.svg"}, "image"},
		{"archive tgz", repos.Entry{Name: "release.tar.gz"}, "archive"},
		{"archive xz", repos.Entry{Name: "release.tar.xz"}, "archive"},
		{"config yaml", repos.Entry{Name: "ci.YAML"}, "config"},
		{"config toml", repos.Entry{Name: "settings.toml"}, "config"},
		{"pdf", repos.Entry{Name: "manual.pdf"}, "pdf"},
		{"markup html", repos.Entry{Name: "index.html"}, "markup"},
		{"markup css", repos.Entry{Name: "site.css"}, "markup"},
		{"lockfile package-lock", repos.Entry{Name: "package-lock.json"}, "lockfile"},
		{"lockfile yarn", repos.Entry{Name: "yarn.lock"}, "lockfile"},
		{"lockfile go.sum", repos.Entry{Name: "go.sum"}, "lockfile"},
		{"lockfile beats json ext", repos.Entry{Name: "pnpm-lock.yaml"}, "lockfile"},
		{"dotfile gitignore", repos.Entry{Name: ".GitIgnore"}, "dotfile"},
		{"dotfile env", repos.Entry{Name: ".env"}, "dotfile"},
		{"unknown extension", repos.Entry{Name: "data.bin"}, "file"},
		{"no extension", repos.Entry{Name: "Makefile"}, "file"},
		{"leading dot only", repos.Entry{Name: ".hidden"}, "file"},
		{"dir dots", repos.Entry{Name: "..."}, "file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := iconFor(tc.entry); got != tc.want {
				t.Errorf("iconFor(%+v) = %q, want %q", tc.entry, got, tc.want)
			}
		})
	}
}

func TestTreeListingIconsAndParentRow(t *testing.T) {
	body := request(newTestHandler(t, readerIdentity(), browseStore()), "GET", "/acme/widget/tree").Body.String()

	for _, want := range []string{
		`<svg class="icon" aria-hidden="true"><use href="#icon-folder"></use></svg></td>`, // smoke: sprite use renders
		`href="/acme/widget/blob/README.md"`,
		`icon-doc`,
		`icon-markup`,
		`icon-submodule`,
		`icon-symlink`,
		`icon-executable`,
		`visually-hidden">directory: </span>docs/`,
		`visually-hidden">file: </span>bin.dat`,
		`id="icon-file"`,
		`id="icon-folder"`,
		`id="icon-symlink"`,
		`id="icon-submodule"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("root tree page missing %q", want)
		}
	}
	if strings.Contains(body, `href="/acme/widget/tree/docs/"`) {
		t.Error("directory href must not carry the trailing slash")
	}

	for _, unwanted := range []string{
		`<th scope="col">Type</th>`,
		`data-label="Type"`,
		`parent-link`,
		`Parent directory`,
	} {
		if strings.Contains(body, unwanted) {
			t.Errorf("root tree page unexpectedly contains %q", unwanted)
		}
	}
	if strings.Contains(body, `href="../"`) {
		t.Error("root tree page must not render a parent row")
	}
	if strings.Contains(body, ">../</a>") {
		t.Error("root tree page must not render a ../ row")
	}
}

func TestTreeListingParentRow(t *testing.T) {
	body := request(newTestHandler(t, readerIdentity(), browseStore()), "GET", "/acme/widget/tree/docs?ref=feature").Body.String()

	wantHref := `href="/acme/widget/tree?ref=feature">../</a>`
	if !strings.Contains(body, wantHref) {
		t.Errorf("nested tree page missing parent row with ref-carrying href %q:\n%s", wantHref, body)
	}
	if !strings.Contains(body, `icon-folder`) {
		t.Error("parent row should use the folder icon")
	}
}

func TestEmptyNonRootTreeRendersParentRow(t *testing.T) {
	store := browseStore()
	base := store.onTree
	store.onTree = func(name, commit, path string) ([]repos.Entry, error) {
		if path == "emptydir" {
			return []repos.Entry{}, nil
		}
		return base(name, commit, path)
	}
	body := request(newTestHandler(t, readerIdentity(), store), "GET", "/acme/widget/tree/emptydir").Body.String()

	if !strings.Contains(body, `class="tree"`) {
		t.Errorf("empty non-root directory should still render the table:\n%s", body)
	}
	if !strings.Contains(body, `>../</a>`) {
		t.Errorf("empty non-root directory should render the ../ row:\n%s", body)
	}
	if strings.Contains(body, "This directory is empty.") {
		t.Error("empty non-root directory should not hide the table behind the empty-state message")
	}
}

func TestEmptyRootTreeShowsEmptyState(t *testing.T) {
	store := browseStore()
	store.onTree = func(_, _, _ string) ([]repos.Entry, error) {
		return []repos.Entry{}, nil
	}
	body := request(newTestHandler(t, readerIdentity(), store), "GET", "/acme/widget/tree").Body.String()

	if !strings.Contains(body, "This directory is empty.") {
		t.Errorf("empty root should render the empty-state message:\n%s", body)
	}
	if strings.Contains(body, `class="tree"`) {
		t.Error("empty root should not render the table")
	}
}
