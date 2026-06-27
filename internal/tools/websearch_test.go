package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/francescoalemanno/raijin-mono/libagent"
)

const ddgFixture = `<div class="result results_links results_links_deep web-result ">
<a rel="nofollow" class="result__a" href="https://example.com/a">First <b>Result</b></a>
<a class="result__snippet" href="https://example.com/a">Snippet for <b>first</b> result.</a>
</div>
<div class="result results_links results_links_deep web-result ">
<a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fb">Second Result</a>
<a class="result__snippet" href="https://example.com/b">Second snippet.</a>
</div>`

func TestWebsearchRequiresQuery(t *testing.T) {
	t.Parallel()

	tool := NewWebsearchTool()
	resp, err := tool.Run(context.Background(), libagent.ToolCall{Input: `{}`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("expected error response")
	}
}

func TestParseDDGHTML(t *testing.T) {
	t.Parallel()

	results := parseDDGHTML(ddgFixture, 10)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Title != "First Result" {
		t.Fatalf("title[0] = %q", results[0].Title)
	}
	if results[0].Snippet != "Snippet for first result." {
		t.Fatalf("snippet[0] = %q", results[0].Snippet)
	}
	if results[1].URL != "https://example.com/b" {
		t.Fatalf("url[1] = %q", results[1].URL)
	}
}

func TestSearchWebUsesParser(t *testing.T) {
	t.Parallel()

	origFetch := fetchDDGHTMLFn
	t.Cleanup(func() { fetchDDGHTMLFn = origFetch })
	fetchDDGHTMLFn = func(_ context.Context, _ string) (string, error) {
		return ddgFixture, nil
	}

	results, err := searchWeb(context.Background(), "fixture query", 5)
	if err != nil {
		t.Fatalf("searchWeb: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestWebsearchToolFormatsOutput(t *testing.T) {
	t.Parallel()

	origFetch := fetchDDGHTMLFn
	t.Cleanup(func() { fetchDDGHTMLFn = origFetch })
	fetchDDGHTMLFn = func(_ context.Context, _ string) (string, error) {
		return ddgFixture, nil
	}

	tool := NewWebsearchTool()
	resp, err := tool.Run(context.Background(), libagent.ToolCall{Input: `{"query":"fixture query"}`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.IsError {
		t.Fatalf("unexpected error response: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "First Result") {
		t.Fatalf("expected formatted output, got %q", resp.Content)
	}
}
