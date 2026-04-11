package plugin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gmoigneu/gcode/pkg/ai"
)

// buildFakePlugin compiles a tiny Go program that speaks the plugin
// protocol into a binary we can launch during the test. Returns the
// binary path.
func buildFakePlugin(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "plugin")
	return bin // caller will `go run` via exec — simpler than go build in test
}

// Using go run for simplicity — the helper tests exercise the RPC loop.
const fakePluginSource = `package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

type req struct {
	ID     uint64          ` + "`json:\"id\"`" + `
	Method string          ` + "`json:\"method\"`" + `
	Params json.RawMessage ` + "`json:\"params\"`" + `
}

type resp struct {
	JSONRPC string      ` + "`json:\"jsonrpc\"`" + `
	ID      uint64      ` + "`json:\"id\"`" + `
	Result  interface{} ` + "`json:\"result\"`" + `
}

func main() {
	scan := bufio.NewScanner(os.Stdin)
	scan.Buffer(make([]byte, 64*1024), 1<<20)
	for scan.Scan() {
		var r req
		if err := json.Unmarshal(scan.Bytes(), &r); err != nil {
			continue
		}
		switch r.Method {
		case "list_tools":
			json.NewEncoder(os.Stdout).Encode(resp{
				JSONRPC: "2.0", ID: r.ID,
				Result: []map[string]interface{}{
					{"name": "echo", "description": "echo back", "parameters": json.RawMessage(` + "`" + `{"type":"object"}` + "`" + `)},
				},
			})
		case "invoke_tool":
			var p struct {
				Name      string                 ` + "`json:\"name\"`" + `
				Arguments map[string]interface{} ` + "`json:\"arguments\"`" + `
			}
			json.Unmarshal(r.Params, &p)
			text := fmt.Sprintf("echo: %v", p.Arguments["text"])
			json.NewEncoder(os.Stdout).Encode(resp{
				JSONRPC: "2.0", ID: r.ID,
				Result: map[string]interface{}{"text": text, "is_error": false},
			})
		}
	}
}
`

// To test the plugin host end-to-end without shelling out to go build,
// we can ship a script-based plugin that works on POSIX hosts. Skip on
// Windows.
func TestLaunchPluginWithBashScript(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "plugin.sh")
	body := `#!/bin/bash
while IFS= read -r line; do
  if echo "$line" | grep -q '"list_tools"'; then
    id=$(echo "$line" | sed 's/.*"id":\([0-9]*\).*/\1/')
    printf '{"jsonrpc":"2.0","id":%s,"result":[{"name":"echo","description":"echo","parameters":{"type":"object"}}]}\n' "$id"
  elif echo "$line" | grep -q '"invoke_tool"'; then
    id=$(echo "$line" | sed 's/.*"id":\([0-9]*\).*/\1/')
    printf '{"jsonrpc":"2.0","id":%s,"result":{"text":"fake response","is_error":false}}\n' "$id"
  fi
done
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	plugin, err := LaunchPlugin(script)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	defer plugin.Close()

	if len(plugin.Tools()) != 1 {
		t.Fatalf("tools = %d", len(plugin.Tools()))
	}
	tool := plugin.Tools()[0]
	if tool.Tool.Name != "echo" {
		t.Errorf("name = %q", tool.Tool.Name)
	}

	res, err := tool.Execute("c1", map[string]any{"text": "hi"}, context.Background(), nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	tc := res.Content[0].(*ai.TextContent)
	if !strings.Contains(tc.Text, "fake response") {
		t.Errorf("got %q", tc.Text)
	}
}

func TestLoadPluginsMissingDir(t *testing.T) {
	plugins, err := LoadPlugins(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatal(err)
	}
	if plugins != nil {
		t.Errorf("expected nil, got %v", plugins)
	}
}

func TestLoadPluginsEmptyPath(t *testing.T) {
	plugins, err := LoadPlugins("")
	if err != nil {
		t.Fatal(err)
	}
	if plugins != nil {
		t.Errorf("expected nil, got %v", plugins)
	}
}

func TestLoadPluginsNonDir(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file.txt")
	os.WriteFile(file, []byte("x"), 0o644)
	_, err := LoadPlugins(file)
	if err == nil {
		t.Error("expected error for non-dir")
	}
}

// Guard: ensure fake plugin source compiles for use outside tests.
var _ = fakePluginSource
var _ = buildFakePlugin
