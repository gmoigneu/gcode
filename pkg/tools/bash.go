package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/gmoigneu/gcode/pkg/agent"
	"github.com/gmoigneu/gcode/pkg/ai"
)

// BashParams is the bash tool schema.
type BashParams struct {
	Command string `json:"command" description:"Bash command to execute"`
	Timeout *int   `json:"timeout,omitempty" description:"Timeout in seconds. Default: no timeout"`
}

// NewBashTool returns the bash tool with its working directory pinned to cwd.
func NewBashTool(cwd string) *agent.AgentTool {
	return &agent.AgentTool{
		Tool: ai.Tool{
			Name:        "bash",
			Description: "Execute a bash command. Output is streamed and truncated to the last 50KB / 2000 lines.",
			Parameters:  ai.SchemaFrom[BashParams](),
		},
		Label: "bash",
		Execute: func(id string, params map[string]any, signal context.Context, onUpdate agent.AgentToolUpdateFunc) (agent.AgentToolResult, error) {
			var p BashParams
			if err := decodeParams(params, &p); err != nil {
				return agent.AgentToolResult{}, err
			}
			return executeBash(signal, cwd, p, onUpdate)
		},
	}
}

// executeBash runs the command and streams partial output. The returned
// AgentToolResult always carries a non-nil TextContent with the (possibly
// truncated) output. An error result + non-nil error indicates the command
// itself failed to start or exited non-zero.
func executeBash(parent context.Context, cwd string, p BashParams, onUpdate agent.AgentToolUpdateFunc) (agent.AgentToolResult, error) {
	wrap := func(text string) agent.AgentToolResult {
		return agent.AgentToolResult{
			Content: []ai.Content{&ai.TextContent{Text: text}},
		}
	}
	if p.Command == "" {
		return wrap(""), errors.New("bash: command is empty")
	}

	ctx := parent
	var cancel context.CancelFunc
	if p.Timeout != nil && *p.Timeout > 0 {
		ctx, cancel = context.WithTimeout(parent, time.Duration(*p.Timeout)*time.Second)
	} else {
		ctx, cancel = context.WithCancel(parent)
	}
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", p.Command)
	cmd.Dir = cwd
	applyProcessGroup(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return wrap(""), fmt.Errorf("bash: stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return wrap(""), fmt.Errorf("bash: stderr pipe: %w", err)
	}

	startErr := cmd.Start()
	if startErr != nil {
		// If start failed because the parent context was cancelled, surface
		// a friendly abort marker rather than the raw "context canceled".
		msg := startErr.Error()
		if errors.Is(parent.Err(), context.Canceled) {
			msg = "[Command aborted.]"
		}
		return wrap(msg), fmt.Errorf("bash: start: %w", startErr)
	}

	buf := &rollingBuffer{maxBytes: DefaultMaxBytes * 2}

	var pumpWG sync.WaitGroup
	pumpWG.Add(2)
	go pump(stdout, buf, onUpdate, &pumpWG)
	go pump(stderr, buf, onUpdate, &pumpWG)
	pumpWG.Wait()

	runErr := cmd.Wait()
	if ctx.Err() == context.DeadlineExceeded {
		killProcessTree(cmd)
	}

	output := buf.String()
	tr := TruncateTail(output, nil)

	body := tr.Content
	var suffix string
	if tr.Truncated {
		suffix = fmt.Sprintf("\n\n[Showing last %d lines of %d total (%s of %s).]",
			tr.OutputLines, tr.TotalLines, FormatSize(tr.OutputBytes), FormatSize(tr.TotalBytes))
	}
	if ctx.Err() == context.DeadlineExceeded {
		suffix += fmt.Sprintf("\n\n[Command timed out after %d seconds.]", *p.Timeout)
	} else if errors.Is(parent.Err(), context.Canceled) {
		suffix += "\n\n[Command aborted.]"
	}

	text := body + suffix

	result := agent.AgentToolResult{
		Content: []ai.Content{&ai.TextContent{Text: text}},
		Details: map[string]any{
			"command":  p.Command,
			"exitCode": exitCodeFor(runErr),
		},
	}

	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		// Non-zero exit is reported back as a tool error but still returns
		// the captured output.
		return result, fmt.Errorf("bash: exit %d", exitCodeFor(runErr))
	}
	return result, nil
}

func exitCodeFor(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func pump(r io.Reader, buf *rollingBuffer, onUpdate agent.AgentToolUpdateFunc, wg *sync.WaitGroup) {
	defer wg.Done()
	tmp := make([]byte, 4096)
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
			if onUpdate != nil {
				partial := TruncateTail(buf.String(), nil)
				onUpdate(agent.AgentToolResult{
					Content: []ai.Content{&ai.TextContent{Text: partial.Content}},
				})
			}
		}
		if err != nil {
			return
		}
	}
}

// rollingBuffer caps in-memory command output. Older bytes are dropped
// when the buffer grows beyond maxBytes.
type rollingBuffer struct {
	mu       sync.Mutex
	buf      bytes.Buffer
	maxBytes int
}

func (b *rollingBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Write(p)
	if overflow := b.buf.Len() - b.maxBytes; overflow > 0 {
		// Drop the oldest bytes. bytes.Buffer supports this via Next/Read.
		b.buf.Next(overflow)
	}
	return len(p), nil
}

func (b *rollingBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// ---- process group handling ----

func applyProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killProcessTree(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	// Kill the entire process group. The minus sign targets the PGID.
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_ = cmd.Process.Kill() // best-effort fallback
}

// compile-time assertion that os is referenced for future uses.
var _ = os.Getpid
