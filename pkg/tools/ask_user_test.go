package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gmoigneu/gcode/pkg/ai"
)

func TestAskUserSingleSelection(t *testing.T) {
	handler := QuestionHandlerFunc(func(p AskUserParams) (AskUserResult, error) {
		return AskUserResult{Selected: []string{"yes"}}, nil
	})
	r, err := executeAskUser(context.Background(), handler, AskUserParams{
		Question: "Proceed?",
		Options:  []AskOption{{Title: "yes"}, {Title: "no"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	tc := r.Content[0].(*ai.TextContent)
	if !strings.Contains(tc.Text, "yes") {
		t.Errorf("text = %q", tc.Text)
	}
	details := r.Details.(map[string]any)
	if details["cancelled"] != false {
		t.Errorf("cancelled = %v", details["cancelled"])
	}
}

func TestAskUserMultipleSelection(t *testing.T) {
	handler := QuestionHandlerFunc(func(p AskUserParams) (AskUserResult, error) {
		return AskUserResult{Selected: []string{"a", "b"}}, nil
	})
	multi := true
	r, err := executeAskUser(context.Background(), handler, AskUserParams{
		Question:      "Pick many",
		AllowMultiple: &multi,
	})
	if err != nil {
		t.Fatal(err)
	}
	tc := r.Content[0].(*ai.TextContent)
	if !strings.Contains(tc.Text, "a, b") {
		t.Errorf("text = %q", tc.Text)
	}
}

func TestAskUserFreeformResponse(t *testing.T) {
	handler := QuestionHandlerFunc(func(p AskUserParams) (AskUserResult, error) {
		return AskUserResult{Freeform: "custom answer"}, nil
	})
	r, err := executeAskUser(context.Background(), handler, AskUserParams{Question: "?"})
	if err != nil {
		t.Fatal(err)
	}
	tc := r.Content[0].(*ai.TextContent)
	if !strings.Contains(tc.Text, "custom answer") {
		t.Errorf("text = %q", tc.Text)
	}
}

func TestAskUserCancelled(t *testing.T) {
	handler := QuestionHandlerFunc(func(p AskUserParams) (AskUserResult, error) {
		return AskUserResult{Cancelled: true}, nil
	})
	r, err := executeAskUser(context.Background(), handler, AskUserParams{Question: "?"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Details.(map[string]any)["cancelled"] != true {
		t.Error("expected cancelled=true")
	}
	tc := r.Content[0].(*ai.TextContent)
	if !strings.Contains(tc.Text, "cancelled") {
		t.Errorf("text = %q", tc.Text)
	}
}

func TestAskUserContextPassedThrough(t *testing.T) {
	var got AskUserParams
	handler := QuestionHandlerFunc(func(p AskUserParams) (AskUserResult, error) {
		got = p
		return AskUserResult{Freeform: "ok"}, nil
	})
	_, err := executeAskUser(context.Background(), handler, AskUserParams{
		Question: "Question",
		Context:  "Background info",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Context != "Background info" {
		t.Errorf("context = %q", got.Context)
	}
}

func TestAskUserDefaultAllowFreeform(t *testing.T) {
	var received AskUserParams
	handler := QuestionHandlerFunc(func(p AskUserParams) (AskUserResult, error) {
		received = p
		return AskUserResult{}, nil
	})
	_, err := executeAskUser(context.Background(), handler, AskUserParams{Question: "?"})
	if err != nil {
		t.Fatal(err)
	}
	if received.AllowFreeform == nil || !*received.AllowFreeform {
		t.Errorf("AllowFreeform default = %v", received.AllowFreeform)
	}
}

func TestAskUserRespectsContextCancellation(t *testing.T) {
	handler := QuestionHandlerFunc(func(p AskUserParams) (AskUserResult, error) {
		time.Sleep(200 * time.Millisecond)
		return AskUserResult{Freeform: "late"}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	r, err := executeAskUser(ctx, handler, AskUserParams{Question: "?"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Details.(map[string]any)["cancelled"] != true {
		t.Error("cancellation not reported")
	}
}

func TestAskUserHandlerError(t *testing.T) {
	handler := QuestionHandlerFunc(func(p AskUserParams) (AskUserResult, error) {
		return AskUserResult{}, errors.New("io failure")
	})
	_, err := executeAskUser(context.Background(), handler, AskUserParams{Question: "?"})
	if err == nil || !strings.Contains(err.Error(), "io failure") {
		t.Errorf("err = %v", err)
	}
}

func TestAskUserNoHandler(t *testing.T) {
	_, err := executeAskUser(context.Background(), nil, AskUserParams{Question: "?"})
	if err == nil {
		t.Error("expected error")
	}
}

func TestAskUserEmptyQuestion(t *testing.T) {
	handler := QuestionHandlerFunc(func(p AskUserParams) (AskUserResult, error) {
		return AskUserResult{}, nil
	})
	_, err := executeAskUser(context.Background(), handler, AskUserParams{Question: ""})
	if err == nil {
		t.Error("expected error")
	}
}
