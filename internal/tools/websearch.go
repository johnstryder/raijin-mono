package tools

import (
	"context"
	"fmt"
	htmlstd "html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/francescoalemanno/raijin-mono/libagent"
)

const (
	websearchDescription = "Search the web for up-to-date information. Returns titles, URLs, and snippets. Use for current events, release notes, documentation, or facts not available in the workspace."
	websearchUserAgent   = "Mozilla/5.0 (compatible; Raijin/1.0; +https://github.com/francescoalemanno/raijin-mono)"
	ddgSearchEndpoint    = "https://html.duckduckgo.com/html/"
	defaultWebsearchMax  = 8
	maxWebsearchMax      = 20
	websearchTimeout     = 20 * time.Second
)

// ponytail: DDG HTML scraping; upgrade path is a keyed provider (Brave/Tavily) via env.
var (
	ddgResultRE    = regexp.MustCompile(`(?s)<div class="result results_links[^"]*">.*?<a rel="nofollow" class="result__a" href="([^"]*)">(.*?)</a>.*?<a class="result__snippet"[^>]*>(.*?)</a>`)
	fetchDDGHTMLFn = fetchDDGHTML
)

type websearchParams struct {
	Query      string `json:"query" description:"Search query"`
	MaxResults int    `json:"max_results,omitempty" description:"Maximum number of results to return (default: 8, max: 20)"`
}

type webSearchResult struct {
	Title   string
	URL     string
	Snippet string
}

func RenderWebsearchSingleLinePreview(toolCallParams string) string {
	return renderSingleLineForTool("websearch", toolCallParams, renderWebsearchToolPreview)
}

func renderWebsearchToolPreview(name string, params map[string]any) string {
	query := stringParam(params, "query")
	if query == "" {
		return renderGenericPreview(name, params)
	}
	if maxResults := intParam(params, "max_results"); maxResults > 0 {
		return fmt.Sprintf("%s %s (max=%d)", name, quoteIfNeeded(query), maxResults)
	}
	return fmt.Sprintf("%s %s", name, quoteIfNeeded(query))
}

// NewWebsearchTool creates a web search tool backed by DuckDuckGo HTML results.
func NewWebsearchTool() libagent.Tool {
	handler := func(ctx context.Context, params websearchParams, _ libagent.ToolCall) (libagent.ToolResponse, error) {
		query := strings.TrimSpace(params.Query)
		if query == "" {
			return libagent.NewTextErrorResponse("query is required"), nil
		}

		maxResults := params.MaxResults
		if maxResults <= 0 {
			maxResults = defaultWebsearchMax
		}
		if maxResults > maxWebsearchMax {
			maxResults = maxWebsearchMax
		}

		results, err := searchWeb(ctx, query, maxResults)
		if err != nil {
			if ctx.Err() != nil {
				return libagent.ToolResponse{}, ctx.Err()
			}
			return libagent.NewTextErrorResponse(err.Error()), nil
		}
		if len(results) == 0 {
			return libagent.NewTextResponse(fmt.Sprintf("No results found for: %s", query)), nil
		}

		var out strings.Builder
		fmt.Fprintf(&out, "Web search results for %q:\n\n", query)
		for i, result := range results {
			fmt.Fprintf(&out, "%d. %s\n   %s\n   %s\n", i+1, result.Title, result.URL, result.Snippet)
			if i+1 < len(results) {
				out.WriteByte('\n')
			}
		}
		return libagent.NewTextResponse(out.String()), nil
	}

	return WrapTool(
		libagent.NewParallelTypedTool("websearch", websearchDescription, handler),
		RenderWebsearchSingleLinePreview,
		nil,
	)
}

func searchWeb(ctx context.Context, query string, maxResults int) ([]webSearchResult, error) {
	body, err := fetchDDGHTMLFn(ctx, query)
	if err != nil {
		return nil, err
	}
	return parseDDGHTML(body, maxResults), nil
}

func fetchDDGHTML(ctx context.Context, query string) (string, error) {
	form := url.Values{}
	form.Set("q", query)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ddgSearchEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", websearchUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Origin", "https://html.duckduckgo.com")
	req.Header.Set("Referer", "https://html.duckduckgo.com/")

	client := &http.Client{
		Timeout:   websearchTimeout,
		Transport: &http.Transport{Proxy: http.ProxyFromEnvironment},
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("web search request failed: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusAccepted, http.StatusTooManyRequests:
		return "", fmt.Errorf("web search rate-limited (HTTP %d); wait and retry", resp.StatusCode)
	case http.StatusOK:
		// continue
	default:
		return "", fmt.Errorf("web search failed: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return "", fmt.Errorf("read web search response: %w", err)
	}
	return string(data), nil
}

func parseDDGHTML(body string, maxResults int) []webSearchResult {
	matches := ddgResultRE.FindAllStringSubmatch(body, maxResults)
	results := make([]webSearchResult, 0, len(matches))
	for _, match := range matches {
		if len(match) < 4 {
			continue
		}
		title := cleanHTMLText(match[2])
		href := unwrapDDGRedirect(strings.TrimSpace(match[1]))
		snippet := cleanHTMLText(match[3])
		if title == "" || href == "" {
			continue
		}
		results = append(results, webSearchResult{
			Title:   title,
			URL:     href,
			Snippet: snippet,
		})
	}
	return results
}

func cleanHTMLText(raw string) string {
	text := htmlstd.UnescapeString(raw)
	text = strings.ReplaceAll(text, "<b>", "")
	text = strings.ReplaceAll(text, "</b>", "")
	text = strings.Join(strings.Fields(text), " ")
	return strings.TrimSpace(text)
}

func unwrapDDGRedirect(href string) string {
	if href == "" {
		return href
	}
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}
	u, err := url.Parse(href)
	if err != nil || !strings.Contains(u.Host, "duckduckgo.com") {
		return href
	}
	if uddg := u.Query().Get("uddg"); uddg != "" {
		if decoded, err := url.QueryUnescape(uddg); err == nil {
			return decoded
		}
	}
	return href
}
