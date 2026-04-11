package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gmoigneu/gcode/pkg/agent"
	"github.com/gmoigneu/gcode/pkg/ai"
)

// FetchParams is the schema of the fetch tool.
type FetchParams struct {
	URL     string            `json:"url" description:"URL to fetch"`
	Method  string            `json:"method,omitempty" description:"HTTP method. Default: GET"`
	Headers map[string]string `json:"headers,omitempty" description:"Request headers"`
	Body    string            `json:"body,omitempty" description:"Request body"`
	Timeout *int              `json:"timeout,omitempty" description:"Timeout in seconds. Default: 30"`
}

// fetchHTTPClient is the client used by NewFetchTool. Tests override it with
// the client from httptest.NewServer.
var fetchHTTPClient = &http.Client{
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("stopped after %d redirects", len(via))
		}
		return nil
	},
}

// NewFetchTool returns the fetch tool. It makes raw HTTP requests and
// returns the status line + headers + truncated body.
func NewFetchTool() *agent.AgentTool {
	return &agent.AgentTool{
		Tool: ai.Tool{
			Name:        "fetch",
			Description: "Fetch a URL. Returns status, headers, and body (truncated to 50KB / 2000 lines).",
			Parameters:  ai.SchemaFrom[FetchParams](),
		},
		Label: "fetch",
		Execute: func(id string, params map[string]any, signal context.Context, onUpdate agent.AgentToolUpdateFunc) (agent.AgentToolResult, error) {
			var p FetchParams
			if err := decodeParams(params, &p); err != nil {
				return agent.AgentToolResult{}, err
			}
			return executeFetch(signal, p)
		},
	}
}

func executeFetch(parent context.Context, p FetchParams) (agent.AgentToolResult, error) {
	if p.URL == "" {
		return agent.AgentToolResult{}, errors.New("fetch: url is empty")
	}
	method := strings.ToUpper(strings.TrimSpace(p.Method))
	if method == "" {
		method = "GET"
	}

	timeout := 30 * time.Second
	if p.Timeout != nil && *p.Timeout > 0 {
		timeout = time.Duration(*p.Timeout) * time.Second
	}

	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	var body io.Reader
	if p.Body != "" {
		body = strings.NewReader(p.Body)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.URL, body)
	if err != nil {
		return agent.AgentToolResult{}, fmt.Errorf("fetch: build request: %w", err)
	}

	req.Header.Set("User-Agent", "gcode/1.0")
	req.Header.Set("Accept", "*/*")
	for k, v := range p.Headers {
		req.Header.Set(k, v)
	}

	resp, err := fetchHTTPClient.Do(req)
	if err != nil {
		return agent.AgentToolResult{}, fmt.Errorf("fetch: http: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return agent.AgentToolResult{}, fmt.Errorf("fetch: read body: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	isBinary := isBinaryContentType(contentType)

	var bodyText string
	var truncatedHint string
	var binaryHint string

	if isBinary {
		binaryHint = fmt.Sprintf("[Binary content (%s, %s). Body omitted.]", contentType, FormatSize(len(raw)))
	} else {
		tr := TruncateHead(string(raw), nil)
		bodyText = tr.Content
		if tr.Truncated {
			truncatedHint = fmt.Sprintf("\n\n[Body truncated: showing %d of %d lines, %s of %s]",
				tr.OutputLines, tr.TotalLines, FormatSize(tr.OutputBytes), FormatSize(tr.TotalBytes))
		}
	}

	text := formatFetchResult(resp, bodyText, truncatedHint, binaryHint)

	return agent.AgentToolResult{
		Content: []ai.Content{&ai.TextContent{Text: text}},
		Details: map[string]any{
			"url":         p.URL,
			"status":      resp.StatusCode,
			"contentType": contentType,
			"bytes":       len(raw),
			"binary":      isBinary,
		},
	}, nil
}

func formatFetchResult(resp *http.Response, body, truncatedHint, binaryHint string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", resp.Proto, resp.Status)

	headerKeys := make([]string, 0, len(resp.Header))
	for k := range resp.Header {
		headerKeys = append(headerKeys, k)
	}
	sort.Strings(headerKeys)
	for _, k := range headerKeys {
		for _, v := range resp.Header.Values(k) {
			fmt.Fprintf(&b, "%s: %s\n", k, v)
		}
	}
	b.WriteString("\n")

	if binaryHint != "" {
		b.WriteString(binaryHint)
		return b.String()
	}
	b.WriteString(body)
	b.WriteString(truncatedHint)
	return b.String()
}

func isBinaryContentType(ct string) bool {
	ct = strings.ToLower(ct)
	if ct == "" {
		return false
	}
	if idx := strings.Index(ct, ";"); idx >= 0 {
		ct = ct[:idx]
	}
	ct = strings.TrimSpace(ct)
	// Text-ish types are not binary.
	if strings.HasPrefix(ct, "text/") {
		return false
	}
	switch ct {
	case "application/json", "application/xml", "application/xhtml+xml", "application/javascript", "application/x-www-form-urlencoded":
		return false
	}
	if strings.HasSuffix(ct, "+json") || strings.HasSuffix(ct, "+xml") {
		return false
	}
	return true
}
