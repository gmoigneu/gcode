package tools

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gmoigneu/gcode/pkg/ai"
)

func TestFetchGetReturnsStatusHeadersBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("X-Custom", "hello")
		_, _ = w.Write([]byte("response body"))
	}))
	defer server.Close()

	old := fetchHTTPClient
	fetchHTTPClient = server.Client()
	t.Cleanup(func() { fetchHTTPClient = old })

	r, err := executeFetch(context.Background(), FetchParams{URL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	tc := r.Content[0].(*ai.TextContent)
	if !strings.Contains(tc.Text, "200") {
		t.Errorf("status missing: %q", tc.Text)
	}
	if !strings.Contains(tc.Text, "X-Custom: hello") {
		t.Errorf("header missing: %q", tc.Text)
	}
	if !strings.Contains(tc.Text, "response body") {
		t.Errorf("body missing: %q", tc.Text)
	}
}

func TestFetchPostWithBody(t *testing.T) {
	var gotBody string
	var gotContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	old := fetchHTTPClient
	fetchHTTPClient = server.Client()
	t.Cleanup(func() { fetchHTTPClient = old })

	_, err := executeFetch(context.Background(), FetchParams{
		URL:     server.URL,
		Method:  "POST",
		Body:    `{"k":"v"}`,
		Headers: map[string]string{"Content-Type": "application/json"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotBody != `{"k":"v"}` {
		t.Errorf("body = %q", gotBody)
	}
	if gotContentType != "application/json" {
		t.Errorf("content-type = %q", gotContentType)
	}
}

func TestFetchLargeBodyTruncated(t *testing.T) {
	big := strings.Repeat("abcdefghij\n", 10000) // ~110KB
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(big))
	}))
	defer server.Close()

	old := fetchHTTPClient
	fetchHTTPClient = server.Client()
	t.Cleanup(func() { fetchHTTPClient = old })

	r, err := executeFetch(context.Background(), FetchParams{URL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	tc := r.Content[0].(*ai.TextContent)
	if !strings.Contains(tc.Text, "Body truncated") {
		t.Errorf("truncation marker missing")
	}
}

func TestFetchBinaryContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte{0x89, 0x50, 0x4E, 0x47})
	}))
	defer server.Close()

	old := fetchHTTPClient
	fetchHTTPClient = server.Client()
	t.Cleanup(func() { fetchHTTPClient = old })

	r, err := executeFetch(context.Background(), FetchParams{URL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	tc := r.Content[0].(*ai.TextContent)
	if !strings.Contains(tc.Text, "Binary content") {
		t.Errorf("binary marker missing: %q", tc.Text)
	}
}

func TestFetchFollowsRedirects(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("final"))
	}))
	defer target.Close()

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirect.Close()

	old := fetchHTTPClient
	fetchHTTPClient = redirect.Client()
	t.Cleanup(func() { fetchHTTPClient = old })

	r, err := executeFetch(context.Background(), FetchParams{URL: redirect.URL})
	if err != nil {
		t.Fatal(err)
	}
	tc := r.Content[0].(*ai.TextContent)
	if !strings.Contains(tc.Text, "final") {
		t.Errorf("redirect target body missing: %q", tc.Text)
	}
}

func TestFetchDefaultUserAgent(t *testing.T) {
	var ua string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.Header.Get("User-Agent")
	}))
	defer server.Close()

	old := fetchHTTPClient
	fetchHTTPClient = server.Client()
	t.Cleanup(func() { fetchHTTPClient = old })

	_, err := executeFetch(context.Background(), FetchParams{URL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ua, "gcode/") {
		t.Errorf("ua = %q", ua)
	}
}

func TestFetchInvalidURL(t *testing.T) {
	_, err := executeFetch(context.Background(), FetchParams{URL: "://not a url"})
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestFetchEmptyURL(t *testing.T) {
	_, err := executeFetch(context.Background(), FetchParams{URL: ""})
	if err == nil {
		t.Error("expected error for empty URL")
	}
}

func TestFetchCustomTimeout(t *testing.T) {
	// Verify that the timeout param plumbs through without network.
	timeout := 1
	_, err := executeFetch(context.Background(), FetchParams{URL: "http://127.0.0.1:1", Timeout: &timeout})
	if err == nil {
		t.Error("expected connection error")
	}
}

func TestIsBinaryContentType(t *testing.T) {
	cases := map[string]bool{
		"text/plain":               false,
		"text/html; charset=utf-8": false,
		"application/json":         false,
		"application/vnd.api+json": false,
		"application/xml":          false,
		"image/png":                true,
		"application/octet-stream": true,
		"":                         false,
	}
	for ct, want := range cases {
		if got := isBinaryContentType(ct); got != want {
			t.Errorf("isBinaryContentType(%q) = %v, want %v", ct, got, want)
		}
	}
}
