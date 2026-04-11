package main

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestBinaryBuilds verifies the package compiles as a standalone
// binary. This is the most basic smoke test — if it fails the whole
// project is broken.
func TestBinaryBuilds(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("smoke test uses POSIX paths")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "gcode")
	out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("build failed: %v\n%s", err, string(out))
	}
	info, err := exec.Command(bin, "--help").CombinedOutput()
	if err != nil {
		t.Fatalf("--help failed: %v\n%s", err, string(info))
	}
	if !strings.Contains(string(info), "Usage: gcode") {
		t.Errorf("help output missing: %q", string(info))
	}
}

// TestHelpFlag checks that --help exits with code 2 (ParseArgs returns
// ok=false for help).
func TestHelpFlagExitsCleanly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses exec")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "gcode")
	if err := exec.Command("go", "build", "-o", bin, ".").Run(); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "--help")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("help exit = %v (stderr=%s)", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Usage: gcode") {
		t.Errorf("stdout missing usage: %q", stdout.String())
	}
}

// TestInvalidModelErrorsInPipeMode verifies error handling when the
// model isn't known.
func TestInvalidModelErrorsInPipeMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "gcode")
	if err := exec.Command("go", "build", "-o", bin, ".").Run(); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "--print", "--model", "nope", "--prompt", "hi")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		t.Error("expected non-zero exit")
	}
	if !strings.Contains(stderr.String(), "model not found") {
		t.Errorf("stderr = %q", stderr.String())
	}
}
