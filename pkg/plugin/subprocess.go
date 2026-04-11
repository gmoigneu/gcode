package plugin

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/gmoigneu/gcode/pkg/agent"
	"github.com/gmoigneu/gcode/pkg/ai"
)

// Plugin represents a running subprocess plugin: an executable that
// speaks gcode's line-delimited JSON-RPC protocol over stdin/stdout.
// Plugins register tools that the agent can call; the plugin host
// proxies every tool invocation back to the subprocess.
type Plugin struct {
	Name string
	Path string

	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Scanner
	tools   []agent.AgentTool
	writeMu sync.Mutex
	nextID  atomic.Uint64
	pending sync.Map // id -> chan jsonRPCResponse
	closed  atomic.Bool
}

// jsonRPCRequest and jsonRPCResponse are the on-the-wire shapes.
type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      uint64 `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// pluginToolDescriptor is the JSON shape returned by a plugin's
// "list_tools" response.
type pluginToolDescriptor struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// LaunchPlugin starts a plugin executable and performs the list-tools
// handshake. The returned Plugin owns the process; callers must invoke
// Close when shutting down.
func LaunchPlugin(path string) (*Plugin, error) {
	cmd := exec.Command(path)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("plugin: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("plugin: stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("plugin: start: %w", err)
	}

	p := &Plugin{
		Name:   filepath.Base(path),
		Path:   path,
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewScanner(stdout),
	}
	p.stdout.Buffer(make([]byte, 64*1024), 1<<20)

	go p.readLoop()

	tools, err := p.handshake()
	if err != nil {
		_ = p.Close()
		return nil, err
	}
	p.tools = tools
	return p, nil
}

// Tools returns the tool list reported by the plugin on handshake.
func (p *Plugin) Tools() []agent.AgentTool { return p.tools }

// Close terminates the plugin subprocess.
func (p *Plugin) Close() error {
	if !p.closed.CompareAndSwap(false, true) {
		return nil
	}
	_ = p.stdin.Close()
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
		_ = p.cmd.Wait()
	}
	return nil
}

// handshake calls list_tools on the plugin and returns the tools it
// reports.
func (p *Plugin) handshake() ([]agent.AgentTool, error) {
	raw, err := p.call("list_tools", nil)
	if err != nil {
		return nil, err
	}
	var descs []pluginToolDescriptor
	if err := json.Unmarshal(raw, &descs); err != nil {
		return nil, fmt.Errorf("plugin: decode list_tools: %w", err)
	}
	var tools []agent.AgentTool
	for _, d := range descs {
		d := d
		tool := agent.AgentTool{
			Tool: ai.Tool{
				Name:        d.Name,
				Description: d.Description,
				Parameters:  d.Parameters,
			},
			Label: d.Name,
			Execute: func(id string, args map[string]any, signal context.Context, _ agent.AgentToolUpdateFunc) (agent.AgentToolResult, error) {
				return p.invoke(signal, d.Name, args)
			},
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

// invoke calls "invoke_tool" on the plugin with the given arguments.
func (p *Plugin) invoke(ctx context.Context, name string, args map[string]any) (agent.AgentToolResult, error) {
	raw, err := p.call("invoke_tool", map[string]any{
		"name":      name,
		"arguments": args,
	})
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	var out struct {
		Text    string `json:"text"`
		IsError bool   `json:"is_error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return agent.AgentToolResult{}, fmt.Errorf("plugin: decode invoke response: %w", err)
	}
	result := agent.AgentToolResult{
		Content: []ai.Content{&ai.TextContent{Text: out.Text}},
	}
	if out.IsError {
		return result, fmt.Errorf("plugin %s: tool %s returned an error", p.Name, name)
	}
	return result, nil
}

// call performs a single synchronous JSON-RPC request. It writes the
// request, registers a pending channel for the response, and blocks
// until the response arrives.
func (p *Plugin) call(method string, params any) (json.RawMessage, error) {
	if p.closed.Load() {
		return nil, fmt.Errorf("plugin: closed")
	}
	id := p.nextID.Add(1)
	req := jsonRPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}

	ch := make(chan jsonRPCResponse, 1)
	p.pending.Store(id, ch)
	defer p.pending.Delete(id)

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("plugin: marshal request: %w", err)
	}
	p.writeMu.Lock()
	_, err = p.stdin.Write(append(body, '\n'))
	p.writeMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("plugin: write: %w", err)
	}

	resp := <-ch
	if resp.Error != nil {
		return nil, fmt.Errorf("plugin: %s", resp.Error.Message)
	}
	return resp.Result, nil
}

// readLoop drains stdout, dispatching responses to pending channels.
func (p *Plugin) readLoop() {
	for p.stdout.Scan() {
		line := p.stdout.Bytes()
		if len(line) == 0 {
			continue
		}
		var resp jsonRPCResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			continue
		}
		if ch, ok := p.pending.Load(resp.ID); ok {
			if c, ok := ch.(chan jsonRPCResponse); ok {
				c <- resp
			}
		}
	}
	// Scanner stopped — close all pending channels with an error.
	p.closed.Store(true)
	p.pending.Range(func(key, value any) bool {
		if c, ok := value.(chan jsonRPCResponse); ok {
			select {
			case c <- jsonRPCResponse{Error: &jsonRPCError{Code: -1, Message: "plugin stdout closed"}}:
			default:
			}
		}
		return true
	})
}

// LoadPlugins scans a directory for executable plugin binaries and
// launches each one.
func LoadPlugins(dir string) ([]*Plugin, error) {
	if dir == "" {
		return nil, nil
	}
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("plugin: path %q is not a directory", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var plugins []*Plugin
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.Mode()&0o111 == 0 {
			continue // not executable
		}
		plugin, err := LaunchPlugin(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		plugins = append(plugins, plugin)
	}
	return plugins, nil
}
