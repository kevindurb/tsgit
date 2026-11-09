package web

import (
	"strings"
	"testing"
)

func TestMarkdownRawHTMLIsInert(t *testing.T) {
	source := []byte("# Title\n\n<script>alert(1)</script>\n\n<img src=x onerror=alert(1)>\n\n<style>body{display:none}</style>\n")
	out, err := renderMarkdown(source, markdownOptions{Repo: "acme/widget"})
	if err != nil {
		t.Fatal(err)
	}
	html := string(out)
	for _, unwanted := range []string{"<script", "<img", "<style", "onerror="} {
		if strings.Contains(html, unwanted) {
			t.Errorf("rendered Markdown contains active markup %q:\n%s", unwanted, html)
		}
	}
	if !strings.Contains(html, "alert(1)") && !strings.Contains(html, "raw HTML omitted") {
		t.Errorf("raw HTML was neither omitted nor shown as text:\n%s", html)
	}
}

func TestMarkdownLinkPolicy(t *testing.T) {
	source := []byte(strings.Join([]string{
		"[relative](docs/guide.md)",
		"[directory](docs/)",
		"[fragment](#section)",
		"[absolute](/admin)",
		"[script](javascript:alert(1))",
		"[data](data:text/html,<b>x</b>)",
		"[escaping](../secret)",
		"[external](https://example.com/path)",
		"[mail](mailto:reader@example.com)",
		"![alt text](https://example.com/pixel.png)",
	}, "\n\n"))
	out, err := renderMarkdown(source, markdownOptions{Repo: "acme/widget", Ref: "feature"})
	if err != nil {
		t.Fatal(err)
	}
	html := string(out)

	for _, want := range []string{
		`href="/acme/widget/blob/docs/guide.md?ref=feature"`,
		`href="/acme/widget/tree/docs?ref=feature"`,
		`href="#section"`,
		`href="https://example.com/path"`,
		`href="mailto:reader@example.com"`,
		"alt text",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered Markdown does not contain %q:\n%s", want, html)
		}
	}
	for _, unwanted := range []string{
		"javascript:",
		"data:text",
		"../secret",
		`href="/admin"`,
		"<img",
		"pixel.png",
	} {
		if strings.Contains(html, unwanted) {
			t.Errorf("rendered Markdown contains unsafe content %q:\n%s", unwanted, html)
		}
	}
	for _, text := range []string{"absolute", "script", "data", "escaping"} {
		if !strings.Contains(html, text) {
			t.Errorf("rejected link %q did not degrade to text:\n%s", text, html)
		}
	}
}

func TestMarkdownDefaultRefAndInvalidUTF8(t *testing.T) {
	out, err := renderMarkdown([]byte("[file](README.md)\n\nbad\xfftext\n"), markdownOptions{Repo: "acme/widget"})
	if err != nil {
		t.Fatal(err)
	}
	html := string(out)
	if !strings.Contains(html, `href="/acme/widget/blob/README.md"`) {
		t.Errorf("default-ref link is wrong:\n%s", html)
	}
	if strings.Contains(html, "?ref=") {
		t.Errorf("default-ref link carries a ref:\n%s", html)
	}
	if !strings.Contains(html, "bad\uFFFDtext") {
		t.Errorf("invalid UTF-8 was not normalized:\n%s", html)
	}
}
