package web

import (
	"bytes"
	"html/template"
	"net/url"
	"path"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// markdownRenderer is configured once. Raw HTML is deliberately not enabled:
// repository Markdown is untrusted.
var markdownRenderer = goldmark.New(
	goldmark.WithExtensions(extension.GFM, extension.Footnote),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
)

type markdownOptions struct {
	// Repo is the canonical <namespace>/<repo> name used to build links.
	Repo string
	// Ref is the link ref to preserve; empty means the default branch.
	Ref string
	// Dir is the directory of the Markdown source inside the repository.
	Dir string
}

// renderMarkdown renders repository Markdown in safe mode. Repository text is
// normalized to UTF-8, and links and images go through an explicit policy.
func renderMarkdown(source []byte, opts markdownOptions) (template.HTML, error) {
	source = []byte(strings.ToValidUTF8(string(source), "\uFFFD"))
	doc := markdownRenderer.Parser().Parse(text.NewReader(source))
	applyLinkPolicy(doc, source, opts)
	var buf bytes.Buffer
	if err := markdownRenderer.Renderer().Render(&buf, source, doc); err != nil {
		return "", err
	}
	return template.HTML(buf.String()), nil
}

type replacement struct {
	parent ast.Node
	node   ast.Node
	text   []byte
}

func applyLinkPolicy(doc ast.Node, source []byte, opts markdownOptions) {
	var replacements []replacement
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch node := node.(type) {
		case *ast.Image:
			replacements = append(replacements, replacement{
				parent: node.Parent(),
				node:   node,
				text:   node.Text(source),
			})
			return ast.WalkSkipChildren, nil
		case *ast.Link:
			destination := string(node.Destination)
			if rewritten, ok := rewriteDestination(destination, opts); ok {
				node.Destination = []byte(rewritten)
				return ast.WalkContinue, nil
			}
			replacements = append(replacements, replacement{
				parent: node.Parent(),
				node:   node,
				text:   node.Text(source),
			})
			return ast.WalkSkipChildren, nil
		default:
			return ast.WalkContinue, nil
		}
	})
	for _, rep := range replacements {
		rep.parent.ReplaceChild(rep.parent, rep.node, ast.NewString(rep.text))
	}
}

// rewriteDestination returns a safe destination. ok is false when the link must
// degrade to its text content.
func rewriteDestination(destination string, opts markdownOptions) (string, bool) {
	destination = strings.TrimSpace(destination)
	if destination == "" {
		return "", false
	}
	u, err := url.Parse(destination)
	if err != nil {
		return "", false
	}
	if u.Scheme != "" {
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "mailto":
			return destination, true
		default:
			return "", false
		}
	}
	// An absolute path names a server route, not a path inside the repository.
	if strings.HasPrefix(destination, "/") {
		return "", false
	}
	// A fragment-only link stays on the same page.
	if u.Path == "" {
		if u.Fragment != "" || u.RawQuery != "" {
			return destination, true
		}
		return "", false
	}

	cleaned := path.Clean(path.Join(opts.Dir, u.Path))
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}
	route := "blob"
	if cleaned == "." || strings.HasSuffix(u.Path, "/") {
		route = "tree"
	}
	if cleaned == "." {
		cleaned = ""
	}
	target := browseURL(opts.Repo, route, cleaned, opts.Ref)
	if u.RawQuery != "" {
		separator := "?"
		if strings.Contains(target, "?") {
			separator = "&"
		}
		target += separator + u.RawQuery
	}
	if fragment := u.EscapedFragment(); fragment != "" {
		target += "#" + fragment
	}
	return target, true
}
