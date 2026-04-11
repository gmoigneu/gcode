package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gmoigneu/gcode/pkg/agent"
	"github.com/gmoigneu/gcode/pkg/ai"
)

func TestBashSimpleCommand(t *testing.T) {
	r, err := executeBash(context.Background(), t.TempDir(), BashParams{Command: "echo hello"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tc := r.Content[0].(*ai.TextContent)
	if !strings.Contains(tc.Text, "hello") {
		t.Errorf("got %q", tc.Text)
	}
	if r.Details.(map[string]any)["exitCode"] != 0 {
		t.Errorf("exit = %+v", r.Details)
	}
}

func TestBashNonZeroExit(t *testing.T) {
	r, err := executeBash(context.Background(), t.TempDir(), BashParams{Command: "exit 7"}, nil)
	if err == nil {
		t.Fatal("expected error on non-zero exit")
	}
	details := r.Details.(map[string]any)
	if details["exitCode"] != 7 {
		t.Errorf("exit = %v", details["exitCode"])
	}
}

func TestBashCapturesStderr(t *testing.T) {
	r, err := executeBash(context.Background(), t.TempDir(), BashParams{Command: "echo out; echo err 1>&2"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tc := r.Content[0].(*ai.TextContent)
	if !strings.Contains(tc.Text, "out") || !strings.Contains(tc.Text, "err") {
		t.Errorf("missing combined output: %q", tc.Text)
	}
}

func TestBashWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	r, err := executeBash(context.Background(), dir, BashParams{Command: "pwd"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tc := r.Content[0].(*ai.TextContent)
	if !strings.Contains(tc.Text, dir) {
		t.Errorf("pwd = %q, want dir containing %q", tc.Text, dir)
	}
}

func TestBashTimeout(t *testing.T) {
	timeout := 1
	r, err := executeBash(context.Background(), t.TempDir(), BashParams{Command: "sleep 5", Timeout: &timeout}, nil)
	// Either an error (exit -1) or a timeout message — both are acceptable.
	_ = err
	tc := r.Content[0].(*ai.TextContent)
	if !strings.Contains(tc.Text, "timed out") {
		t.Errorf("no timeout marker in output: %q", tc.Text)
	}
}

func TestBashAbort(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	r, _ := executeBash(ctx, t.TempDir(), BashParams{Command: "sleep 2"}, nil)
	tc := r.Content[0].(*ai.TextContent)
	if !strings.Contains(tc.Text, "aborted") {
		t.Errorf("no abort marker: %q", tc.Text)
	}
}

func TestBashEmptyCommand(t *testing.T) {
	_, err := executeBash(context.Background(), t.TempDir(), BashParams{Command: ""}, nil)
	if err == nil {
		t.Error("expected error for empty command")
	}
}

func TestBashStreamsPartialUpdates(t *testing.T) {
	var partialSeen int
	onUpdate := func(partial agent.AgentToolResult) {
		partialSeen++
	}
	_, err := executeBash(context.Background(), t.TempDir(),
		BashParams{Command: "for i in 1 2 3; do echo line$i; sleep 0.01; done"},
		onUpdate)
	if err != nil {
		t.Fatal(err)
	}
	if partialSeen == 0 {
		t.Error("expected at least one partial update")
	}
}

func TestBashOutputTruncation(t *testing.T) {
	// Produce more than 50KB of output so TruncateTail kicks in.
	cmd := "yes abcdefghijklmnopqrstuvwxyz | head -c 120000"
	r, err := executeBash(context.Background(), t.TempDir(), BashParams{Command: cmd}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tc := r.Content[0].(*ai.TextContent)
	if !strings.Contains(tc.Text, "Showing last") {
		t.Errorf("truncation marker missing: %q", tc.Text[len(tc.Text)-200:])
	}
}
