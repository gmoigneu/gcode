package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gmoigneu/gcode/internal/config"
	"github.com/gmoigneu/gcode/pkg/ai"
)

// Args is the parsed CLI command line for a single gcode invocation.
type Args struct {
	// Model selection
	Model    string
	Provider string

	// Mode flags
	PrintMode bool
	JSONMode  bool

	// Options
	Prompt          string
	SystemPrompt    string
	ContinueSession string
	Cwd             string
	ThinkingLevel   string

	// Auth
	APIKey string

	// Help flag
	ShowHelp bool
}

// ParseArgs parses command-line flags. Returns the parsed Args and
// true if a run should proceed (false when --help was requested).
func ParseArgs(argv []string) (Args, bool) {
	var a Args
	fs := flag.NewFlagSet("gcode", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	fs.StringVar(&a.Model, "model", "", "model selector: 'provider/id' or bare 'id'")
	fs.StringVar(&a.Model, "m", "", "short for --model")
	fs.StringVar(&a.Provider, "provider", "", "provider override (anthropic, openai, google, ...)")
	fs.BoolVar(&a.PrintMode, "print", false, "non-interactive pipe mode: stream to stdout and exit")
	fs.BoolVar(&a.PrintMode, "p-mode", false, "alias for --print")
	fs.BoolVar(&a.JSONMode, "json", false, "emit events as NDJSON on stdout")
	fs.StringVar(&a.Prompt, "prompt", "", "prompt text (otherwise read from stdin or interactive input)")
	fs.StringVar(&a.Prompt, "p", "", "short for --prompt")
	fs.StringVar(&a.SystemPrompt, "system", "", "override the default system prompt")
	fs.StringVar(&a.ContinueSession, "continue", "", "continue an existing session by ID")
	fs.StringVar(&a.Cwd, "cwd", "", "working directory for the agent (default: current directory)")
	fs.StringVar(&a.ThinkingLevel, "thinking", "", "reasoning level: off|minimal|low|medium|high|xhigh")
	fs.StringVar(&a.APIKey, "api-key", "", "API key (overrides stored + env keys)")
	fs.BoolVar(&a.ShowHelp, "help", false, "show help")
	fs.BoolVar(&a.ShowHelp, "h", false, "short for --help")

	if err := fs.Parse(argv); err != nil {
		fmt.Fprintf(os.Stderr, "gcode: %s\n", err)
		printUsage(os.Stderr)
		return a, false
	}
	if a.ShowHelp {
		printUsage(os.Stdout)
		return a, false
	}
	// Trailing positional args are treated as the prompt (joined with
	// spaces) when --prompt is not already set.
	if a.Prompt == "" && fs.NArg() > 0 {
		a.Prompt = strings.Join(fs.Args(), " ")
	}
	return a, true
}

// printUsage writes a condensed usage block to w.
func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: gcode [flags] [prompt ...]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Flags:")
	fmt.Fprintln(w, "  -m, --model        MODEL   model to use (e.g. claude-opus-4-6 or anthropic/claude-opus-4-6)")
	fmt.Fprintln(w, "      --provider     NAME    override provider (anthropic, openai, google, ...)")
	fmt.Fprintln(w, "  -p, --prompt       TEXT    prompt text (omit to read from stdin or use the TUI)")
	fmt.Fprintln(w, "      --print                non-interactive: stream to stdout and exit")
	fmt.Fprintln(w, "      --json                 emit NDJSON events instead of plain text")
	fmt.Fprintln(w, "      --thinking     LEVEL   off|minimal|low|medium|high|xhigh")
	fmt.Fprintln(w, "      --cwd          DIR     working directory")
	fmt.Fprintln(w, "      --api-key      KEY     override stored/env API key")
	fmt.Fprintln(w, "      --continue     ID      continue an existing session")
	fmt.Fprintln(w, "      --system       TEXT    custom system prompt")
	fmt.Fprintln(w, "  -h, --help                 show this help")
}

// ResolveModel searches the model registry for args.Model. It accepts
// "provider/id" and bare "id" formats.
func ResolveModel(args Args) (ai.Model, error) {
	name := strings.TrimSpace(args.Model)
	if name == "" {
		return ai.Model{}, fmt.Errorf("model is required (use --model)")
	}
	if idx := strings.Index(name, "/"); idx >= 0 {
		provider := ai.Provider(name[:idx])
		id := name[idx+1:]
		if m, ok := ai.GetModel(provider, id); ok {
			return m, nil
		}
		return ai.Model{}, fmt.Errorf("model not found: %s", name)
	}
	if args.Provider != "" {
		if m, ok := ai.GetModel(ai.Provider(args.Provider), name); ok {
			return m, nil
		}
	}
	for _, provider := range ai.GetProviders() {
		if m, ok := ai.GetModel(provider, name); ok {
			return m, nil
		}
	}
	return ai.Model{}, fmt.Errorf("model not found: %s", name)
}

// ResolveAPIKey returns the API key for model, preferring (in order):
//  1. --api-key flag
//  2. ~/.gcode/auth.json
//  3. provider-specific environment variable
func ResolveAPIKey(args Args, model ai.Model, auth config.AuthStorage) string {
	if args.APIKey != "" {
		return args.APIKey
	}
	return auth.GetAPIKey(model.Provider)
}
