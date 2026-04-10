package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gmoigneu/gcode/pkg/ai"
)

// init installs the real run loop, replacing the stub in agent.go.
func init() {
	runLoopFn = runLoop
}

// runLoop drives a single Run invocation. It alternates between streaming an
// assistant response, executing tool calls, and checking the steering /
// follow-up queues.
func runLoop(ctx context.Context, agent *Agent, initialMessages []AgentMessage) error {
	var newMessages []AgentMessage

	agent.emit(AgentEvent{Type: AgentEventStart}, ctx)

	if len(initialMessages) > 0 {
		agent.emit(AgentEvent{Type: TurnStart}, ctx)
		for _, m := range initialMessages {
			agent.emit(AgentEvent{Type: MessageStart, Message: m}, ctx)
			agent.emit(AgentEvent{Type: MessageEnd, Message: m}, ctx)
			newMessages = append(newMessages, m)
		}
	}

	var pending []AgentMessage
	firstTurn := true

	for {
		for {
			if err := ctx.Err(); err != nil {
				if agent.liveness != nil {
					agent.liveness.Stop(ctx)
				}
				agent.emit(AgentEvent{Type: AgentEventEnd, NewMessages: newMessages}, ctx)
				return err
			}

			// Emit any pending messages from the previous iteration (steering
			// or follow-up) as a new turn.
			if !firstTurn || len(pending) > 0 {
				if !firstTurn {
					agent.emit(AgentEvent{Type: TurnStart}, ctx)
				}
				for _, m := range pending {
					agent.emit(AgentEvent{Type: MessageStart, Message: m}, ctx)
					agent.emit(AgentEvent{Type: MessageEnd, Message: m}, ctx)
					newMessages = append(newMessages, m)
				}
				pending = nil
			}
			firstTurn = false

			assistantMsg, streamErr := streamAssistantResponse(ctx, agent)
			if streamErr != nil {
				agent.emit(AgentEvent{Type: TurnEnd, TurnMessage: assistantMsg}, ctx)
				if agent.liveness != nil {
					agent.liveness.Stop(ctx)
				}
				agent.emit(AgentEvent{Type: AgentEventEnd, NewMessages: newMessages}, ctx)
				return streamErr
			}
			if assistantMsg != nil && (assistantMsg.StopReason == ai.StopReasonError ||
				assistantMsg.StopReason == ai.StopReasonAborted) {
				newMessages = append(newMessages, assistantMsg)
				agent.emit(AgentEvent{Type: TurnEnd, TurnMessage: assistantMsg}, ctx)
				if agent.liveness != nil {
					agent.liveness.Stop(ctx)
				}
				agent.emit(AgentEvent{Type: AgentEventEnd, NewMessages: newMessages}, ctx)
				if assistantMsg.StopReason == ai.StopReasonAborted {
					return context.Canceled
				}
				return fmt.Errorf("agent: %s", assistantMsg.ErrorMessage)
			}

			if assistantMsg != nil {
				newMessages = append(newMessages, assistantMsg)
			}

			toolCalls := extractToolCalls(assistantMsg)

			var toolResults []ai.ToolResultMessage
			if len(toolCalls) > 0 {
				toolResults = executeToolCalls(ctx, agent, assistantMsg, toolCalls)
				for i := range toolResults {
					newMessages = append(newMessages, &toolResults[i])
				}
			}

			agent.emit(AgentEvent{
				Type:        TurnEnd,
				TurnMessage: assistantMsg,
				ToolResults: toolResults,
			}, ctx)

			// Steering messages take priority over continuing the tool loop.
			steering := agent.steeringQueue.Drain()
			if len(steering) > 0 {
				pending = steering
				continue
			}

			if len(toolCalls) == 0 {
				break
			}
		}

		followUps := agent.followUpQueue.Drain()
		if len(followUps) == 0 {
			break
		}
		pending = followUps
	}

	if agent.liveness != nil {
		agent.liveness.Stop(ctx)
	}
	agent.emit(AgentEvent{Type: AgentEventEnd, NewMessages: newMessages}, ctx)
	return nil
}

// streamAssistantResponse runs one LLM request and emits the message events.
// It returns the final assistant message (or a synthetic error message) and
// any error from the transform context hook.
func streamAssistantResponse(ctx context.Context, agent *Agent) (*ai.AssistantMessage, error) {
	if agent.liveness != nil {
		agent.liveness.SetStatus(ctx, StatusThinking, "")
	}
	snapshot := agent.stateSnapshot()

	messages := append([]AgentMessage(nil), snapshot.Messages...)
	if agent.config.TransformContext != nil {
		var err error
		messages, err = agent.config.TransformContext(ctx, messages)
		if err != nil {
			return nil, err
		}
	}

	var llmMessages []ai.Message
	if agent.config.ConvertToLLM != nil {
		llmMessages = agent.config.ConvertToLLM(messages)
	} else {
		llmMessages = defaultConvertToLLM(messages)
	}

	llmCtx := ai.Context{
		SystemPrompt: snapshot.SystemPrompt,
		Messages:     llmMessages,
		Tools:        extractToolDefs(snapshot.Tools),
	}

	apiKey := ""
	if agent.config.GetAPIKey != nil {
		apiKey = agent.config.GetAPIKey(snapshot.Model.Provider)
	}

	opts := &ai.SimpleStreamOptions{
		StreamOptions: ai.StreamOptions{
			Signal: ctx,
			APIKey: apiKey,
		},
		Reasoning:       snapshot.ThinkingLevel,
		ThinkingBudgets: agent.config.ThinkingBudgets,
	}

	streamFn := agent.config.StreamFn
	if streamFn == nil {
		streamFn = ai.StreamSimple
	}
	stream := streamFn(snapshot.Model, llmCtx, opts)

	for event := range stream.C {
		switch event.Type {
		case ai.EventStart:
			agent.emit(AgentEvent{Type: MessageStart, Message: event.Partial}, ctx)
		case ai.EventDone:
			agent.emit(AgentEvent{Type: MessageEnd, Message: event.Message}, ctx)
		case ai.EventError:
			agent.emit(AgentEvent{Type: MessageEnd, Message: event.Error}, ctx)
		default:
			ev := event
			agent.emit(AgentEvent{
				Type:                  MessageUpdate,
				Message:               event.Partial,
				AssistantMessageEvent: &ev,
			}, ctx)
		}
	}

	result := stream.Result()
	return &result, nil
}

// extractToolCalls returns all tool calls in an assistant message, in order.
func extractToolCalls(msg *ai.AssistantMessage) []ai.ToolCall {
	if msg == nil {
		return nil
	}
	var out []ai.ToolCall
	for _, c := range msg.Content {
		if tc, ok := c.(*ai.ToolCall); ok {
			out = append(out, *tc)
		}
	}
	return out
}

// extractToolDefs pulls the ai.Tool embedded in each AgentTool for the LLM.
func extractToolDefs(tools []AgentTool) []ai.Tool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]ai.Tool, 0, len(tools))
	for _, t := range tools {
		out = append(out, t.Tool)
	}
	return out
}

// defaultConvertToLLM filters out errored/aborted assistant messages and
// passes everything else through unchanged.
func defaultConvertToLLM(messages []AgentMessage) []ai.Message {
	var out []ai.Message
	for _, msg := range messages {
		switch m := msg.(type) {
		case *ai.UserMessage:
			out = append(out, m)
		case *ai.AssistantMessage:
			if m.StopReason == ai.StopReasonError || m.StopReason == ai.StopReasonAborted {
				continue
			}
			out = append(out, m)
		case *ai.ToolResultMessage:
			out = append(out, m)
		}
	}
	return out
}

// executeToolCalls dispatches a slice of tool calls, honouring the agent's
// configured execution mode. Liveness transitions to StatusExecuting before
// dispatch; the next streamAssistantResponse call (or Stop) will flip it
// back to Thinking/Idle.
func executeToolCalls(ctx context.Context, agent *Agent, assistantMsg *ai.AssistantMessage, toolCalls []ai.ToolCall) []ai.ToolResultMessage {
	if agent.liveness != nil && len(toolCalls) > 0 {
		agent.liveness.SetStatus(ctx, StatusExecuting, toolCalls[0].Name)
	}
	switch agent.config.ToolExecution {
	case ToolExecSequential:
		return executeToolCallsSequential(ctx, agent, assistantMsg, toolCalls)
	default:
		return executeToolCallsParallel(ctx, agent, assistantMsg, toolCalls)
	}
}

type toolPrep struct {
	tool     *AgentTool
	toolCall ai.ToolCall
	args     map[string]any
	blocked  bool
	reason   string
}

type toolExecResult struct {
	result AgentToolResult
	err    error
}

// prepareToolCalls runs the sequential prep phase for every tool call:
// lookup, argument preprocessing, beforeToolCall hook, and the tool_execution_start event.
func prepareToolCalls(ctx context.Context, agent *Agent, assistantMsg *ai.AssistantMessage, toolCalls []ai.ToolCall) []toolPrep {
	preps := make([]toolPrep, len(toolCalls))
	snapshot := agent.stateSnapshot()

	for i, tc := range toolCalls {
		agent.emit(AgentEvent{
			Type:       ToolExecutionStart,
			ToolCallID: tc.ID,
			ToolName:   tc.Name,
			ToolArgs:   tc.Arguments,
		}, ctx)

		tool := findTool(snapshot.Tools, tc.Name)
		if tool == nil {
			preps[i] = toolPrep{
				toolCall: tc,
				blocked:  true,
				reason:   fmt.Sprintf("unknown tool: %s", tc.Name),
			}
			continue
		}

		args := tc.Arguments
		if tool.PrepareArguments != nil {
			args = tool.PrepareArguments(args)
		}

		if agent.config.BeforeToolCall != nil {
			res := agent.config.BeforeToolCall(BeforeToolCallContext{
				AssistantMessage: assistantMsg,
				ToolCall:         tc,
				Args:             args,
				Context:          snapshot,
			})
			if res.Block {
				preps[i] = toolPrep{toolCall: tc, blocked: true, reason: res.Reason}
				continue
			}
		}

		preps[i] = toolPrep{tool: tool, toolCall: tc, args: args}
	}
	return preps
}

func executeToolCallsParallel(ctx context.Context, agent *Agent, assistantMsg *ai.AssistantMessage, toolCalls []ai.ToolCall) []ai.ToolResultMessage {
	preps := prepareToolCalls(ctx, agent, assistantMsg, toolCalls)

	results := make(map[int]toolExecResult)
	var resMu sync.Mutex
	var wg sync.WaitGroup

	for i, p := range preps {
		if p.blocked || p.tool == nil {
			continue
		}
		wg.Add(1)
		go func(idx int, prep toolPrep) {
			defer wg.Done()
			onUpdate := func(partial AgentToolResult) {
				agent.emit(AgentEvent{
					Type:       ToolExecutionUpdate,
					ToolCallID: prep.toolCall.ID,
					ToolName:   prep.toolCall.Name,
					ToolArgs:   prep.args,
					ToolResult: &partial,
				}, ctx)
			}
			res, err := prep.tool.Execute(prep.toolCall.ID, prep.args, ctx, onUpdate)
			resMu.Lock()
			results[idx] = toolExecResult{result: res, err: err}
			resMu.Unlock()
		}(i, p)
	}
	wg.Wait()

	return finalizeToolResults(ctx, agent, assistantMsg, preps, results)
}

func executeToolCallsSequential(ctx context.Context, agent *Agent, assistantMsg *ai.AssistantMessage, toolCalls []ai.ToolCall) []ai.ToolResultMessage {
	preps := prepareToolCalls(ctx, agent, assistantMsg, toolCalls)
	results := make(map[int]toolExecResult)

	for i, p := range preps {
		if p.blocked || p.tool == nil {
			continue
		}
		onUpdate := func(partial AgentToolResult) {
			agent.emit(AgentEvent{
				Type:       ToolExecutionUpdate,
				ToolCallID: p.toolCall.ID,
				ToolName:   p.toolCall.Name,
				ToolArgs:   p.args,
				ToolResult: &partial,
			}, ctx)
		}
		res, err := p.tool.Execute(p.toolCall.ID, p.args, ctx, onUpdate)
		results[i] = toolExecResult{result: res, err: err}
	}

	return finalizeToolResults(ctx, agent, assistantMsg, preps, results)
}

// finalizeToolResults converts the prep + execution output into ordered
// ToolResultMessages, invoking the afterToolCall hook and emitting the
// tool_execution_end / message_start / message_end events for each.
func finalizeToolResults(ctx context.Context, agent *Agent, assistantMsg *ai.AssistantMessage, preps []toolPrep, results map[int]toolExecResult) []ai.ToolResultMessage {
	out := make([]ai.ToolResultMessage, 0, len(preps))
	for i, p := range preps {
		tr := ai.ToolResultMessage{
			ToolCallID: p.toolCall.ID,
			ToolName:   p.toolCall.Name,
			Timestamp:  time.Now().UnixMilli(),
		}

		if p.blocked {
			tr.IsError = true
			tr.Content = []ai.Content{&ai.TextContent{Text: p.reason}}
		} else if r, ok := results[i]; ok {
			if r.err != nil {
				tr.IsError = true
				tr.Content = []ai.Content{&ai.TextContent{Text: r.err.Error()}}
			} else {
				tr.Content = r.result.Content
				tr.Details = r.result.Details
			}
		}

		if agent.config.AfterToolCall != nil && !p.blocked {
			override := agent.config.AfterToolCall(AfterToolCallContext{
				BeforeToolCallContext: BeforeToolCallContext{
					AssistantMessage: assistantMsg,
					ToolCall:         p.toolCall,
					Args:             p.args,
				},
				Result:  AgentToolResult{Content: tr.Content, Details: tr.Details},
				IsError: tr.IsError,
			})
			if override.Content != nil {
				tr.Content = override.Content
			}
			if override.Details != nil {
				tr.Details = override.Details
			}
			if override.IsError != nil {
				tr.IsError = *override.IsError
			}
		}

		out = append(out, tr)
	}

	for i := range out {
		tr := &out[i]
		agent.emit(AgentEvent{
			Type:        ToolExecutionEnd,
			ToolCallID:  tr.ToolCallID,
			ToolName:    tr.ToolName,
			ToolResult:  &AgentToolResult{Content: tr.Content, Details: tr.Details},
			ToolIsError: tr.IsError,
		}, ctx)
		agent.emit(AgentEvent{Type: MessageStart, Message: tr}, ctx)
		agent.emit(AgentEvent{Type: MessageEnd, Message: tr}, ctx)
	}
	return out
}

func findTool(tools []AgentTool, name string) *AgentTool {
	for i := range tools {
		if tools[i].Name == name {
			return &tools[i]
		}
	}
	return nil
}
