package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"connectrpc.com/connect"
	claudeagent "github.com/kazz187/claude-agent-sdk-go"
	v1 "github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1"
	"github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1/taskguildv1connect"
)

// interactionWaiter dispatches streamed interaction responses to waiting goroutines.
// It buffers responses that arrive before a waiter registers (race condition between
// CreateInteraction and Register).
type interactionWaiter struct {
	mu      sync.Mutex
	waiters map[string]chan *v1.Interaction // interaction_id -> ch
	pending map[string]*v1.Interaction      // arrived before Register()
}

func newInteractionWaiter() *interactionWaiter {
	return &interactionWaiter{
		waiters: make(map[string]chan *v1.Interaction),
		pending: make(map[string]*v1.Interaction),
	}
}

// Register returns a channel that will receive the responded interaction.
// If a response already arrived (buffered in pending), it is sent immediately.
func (w *interactionWaiter) Register(id string) <-chan *v1.Interaction {
	w.mu.Lock()
	defer w.mu.Unlock()

	ch := make(chan *v1.Interaction, 1)
	if inter, ok := w.pending[id]; ok {
		ch <- inter
		delete(w.pending, id)
	} else {
		w.waiters[id] = ch
	}
	return ch
}

// Unregister removes the waiter for the given interaction ID.
func (w *interactionWaiter) Unregister(id string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.waiters, id)
	delete(w.pending, id)
}

// Deliver is called by the stream listener. If a waiter exists, the interaction
// is sent on the channel; otherwise it is buffered in pending.
func (w *interactionWaiter) Deliver(inter *v1.Interaction) {
	w.mu.Lock()
	defer w.mu.Unlock()

	id := inter.GetId()
	if ch, ok := w.waiters[id]; ok {
		ch <- inter
		delete(w.waiters, id)
	} else {
		w.pending[id] = inter
	}
}

// runInteractionListener subscribes to interaction events for a task and delivers
// responded interactions to the waiter. It returns when the stream ends or ctx is cancelled.
func runInteractionListener(ctx context.Context, interClient taskguildv1connect.InteractionServiceClient, taskID string, waiter *interactionWaiter) {
	stream, err := interClient.SubscribeInteractions(ctx, connect.NewRequest(&v1.SubscribeInteractionsRequest{
		TaskId: taskID,
	}))
	if err != nil {
		log.Printf("[task:%s] interaction stream error: %v", taskID, err)
		return
	}
	defer stream.Close()

	log.Printf("[task:%s] interaction stream connected", taskID)

	for stream.Receive() {
		event := stream.Msg()
		inter := event.GetInteraction()
		if inter == nil {
			continue
		}
		switch inter.GetStatus() {
		case v1.InteractionStatus_INTERACTION_STATUS_RESPONDED:
			log.Printf("[task:%s] interaction %s responded via stream", taskID, inter.GetId())
			waiter.Deliver(inter)
		case v1.InteractionStatus_INTERACTION_STATUS_EXPIRED:
			log.Printf("[task:%s] interaction %s expired via stream", taskID, inter.GetId())
			waiter.Deliver(inter)
		}
	}

	if err := stream.Err(); err != nil && ctx.Err() == nil {
		log.Printf("[task:%s] interaction stream ended: %v", taskID, err)
	}
}

// errWaitTimeout is returned by waitForUserResponse when the user does not
// respond within waitForUserResponseTimeout. The caller should retry with a
// prompt that explicitly asks for NEXT_STATUS.
var errWaitTimeout = fmt.Errorf("user response timeout")

// waitForUserResponse creates a QUESTION interaction and waits for a response via the event stream.
// If the user does not respond within waitForUserResponseTimeout, the interaction is expired
// and errWaitTimeout is returned so the caller can retry.
func waitForUserResponse(
	ctx context.Context,
	client taskguildv1connect.AgentManagerServiceClient,
	interClient taskguildv1connect.InteractionServiceClient,
	taskID string,
	agentManagerID string,
	claudeOutput string,
	waiter *interactionWaiter,
) (string, error) {
	resp, err := client.CreateInteraction(ctx, connect.NewRequest(&v1.CreateInteractionRequest{
		TaskId:      taskID,
		AgentId:     agentManagerID,
		Type:        v1.InteractionType_INTERACTION_TYPE_QUESTION,
		Title:       "Agent needs your input",
		Description: claudeOutput,
	}))
	if err != nil {
		return "", fmt.Errorf("failed to create interaction: %w", err)
	}

	interactionID := resp.Msg.GetInteraction().GetId()
	log.Printf("[task:%s] waiting for user response (interaction: %s)", taskID, interactionID)

	ch := waiter.Register(interactionID)
	defer waiter.Unregister(interactionID)

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(waitForUserResponseTimeout):
		log.Printf("[task:%s] user response timeout (interaction: %s), expiring", taskID, interactionID)
		// Expire the pending interaction so it disappears from the UI.
		if _, expErr := interClient.ExpireInteraction(ctx, connect.NewRequest(&v1.ExpireInteractionRequest{
			Id: interactionID,
		})); expErr != nil {
			log.Printf("[task:%s] failed to expire interaction %s: %v", taskID, interactionID, expErr)
		}
		return "", errWaitTimeout
	case inter := <-ch:
		if inter.GetStatus() == v1.InteractionStatus_INTERACTION_STATUS_EXPIRED {
			return "", fmt.Errorf("interaction expired")
		}
		log.Printf("[task:%s] user responded to interaction %s", taskID, interactionID)
		return inter.GetResponse(), nil
	}
}

// readOnlyTools are always auto-allowed regardless of permission mode.
var readOnlyTools = map[string]bool{
	"Read":      true,
	"Glob":      true,
	"Grep":      true,
	"WebSearch": true,
	"WebFetch":  true,
}

// editTools are auto-allowed in acceptEdits and bypassPermissions modes.
var editTools = map[string]bool{
	"Edit":         true,
	"Write":        true,
	"NotebookEdit": true,
}

// buildPermissionUpdate constructs permission updates for the "always allow" action.
// It prefers CLI-provided suggestions when available, falling back to a manually
// constructed rule from the tool name and input.
func buildPermissionUpdate(toolName string, input map[string]any, suggestions []*claudeagent.PermissionUpdate) []*claudeagent.PermissionUpdate {
	// Prefer CLI suggestions: they contain the exact rule format the CLI expects.
	if len(suggestions) > 0 {
		var updates []*claudeagent.PermissionUpdate
		for _, s := range suggestions {
			if s.Type == claudeagent.PermissionUpdateAddRules &&
				s.Behavior == claudeagent.PermissionBehaviorAllow {
				cp := *s // copy
				cp.Destination = claudeagent.PermissionDestinationLocalSettings
				updates = append(updates, &cp)
			}
		}
		if len(updates) > 0 {
			return updates
		}
	}

	// Fallback: build rule from tool name and input.
	rule := &claudeagent.PermissionRuleValue{
		ToolName: toolName,
	}

	// For Bash, include the command as ruleContent for a specific match.
	if toolName == "Bash" {
		if cmd, ok := input["command"]; ok {
			if cmdStr, ok := cmd.(string); ok && cmdStr != "" {
				rule.RuleContent = cmdStr
			}
		}
	}

	return []*claudeagent.PermissionUpdate{
		{
			Type:        claudeagent.PermissionUpdateAddRules,
			Rules:       []*claudeagent.PermissionRuleValue{rule},
			Behavior:    claudeagent.PermissionBehaviorAllow,
			Destination: claudeagent.PermissionDestinationLocalSettings,
		},
	}
}

// handleAskUserQuestion processes the AskUserQuestion tool by presenting each question
// as an INTERACTION_TYPE_QUESTION with selectable options. Returns PermissionResultAllow
// with the user's answers injected into UpdatedInput.
func handleAskUserQuestion(
	ctx context.Context,
	client taskguildv1connect.AgentManagerServiceClient,
	taskID string,
	agentID string,
	input map[string]any,
	waiter *interactionWaiter,
) (claudeagent.PermissionResult, error) {
	questionsRaw, ok := input["questions"]
	if !ok {
		log.Printf("[task:%s] AskUserQuestion: no questions field", taskID)
		return claudeagent.PermissionResultAllow{}, nil
	}

	questionsSlice, ok := questionsRaw.([]any)
	if !ok {
		log.Printf("[task:%s] AskUserQuestion: questions is not an array", taskID)
		return claudeagent.PermissionResultAllow{}, nil
	}

	answers := make(map[string]any)

	for i, qRaw := range questionsSlice {
		qMap, ok := qRaw.(map[string]any)
		if !ok {
			log.Printf("[task:%s] AskUserQuestion: question[%d] is not a map", taskID, i)
			continue
		}

		questionText, _ := qMap["question"].(string)
		header, _ := qMap["header"].(string)
		if questionText == "" {
			continue
		}

		// Build interaction options from the question's options array.
		var interactionOpts []*v1.InteractionOption
		if optsRaw, ok := qMap["options"].([]any); ok {
			for _, optRaw := range optsRaw {
				optMap, ok := optRaw.(map[string]any)
				if !ok {
					continue
				}
				label, _ := optMap["label"].(string)
				desc, _ := optMap["description"].(string)
				if label == "" {
					continue
				}
				interactionOpts = append(interactionOpts, &v1.InteractionOption{
					Label:       label,
					Value:       label,
					Description: desc,
				})
			}
		}

		// Add "Other" option for free-text input.
		interactionOpts = append(interactionOpts, &v1.InteractionOption{
			Label:       "Other",
			Value:       "__other__",
			Description: "Provide a custom answer",
		})

		// Build description: include header if present.
		description := ""
		if header != "" {
			description = header
		}

		resp, err := client.CreateInteraction(ctx, connect.NewRequest(&v1.CreateInteractionRequest{
			TaskId:      taskID,
			AgentId:     agentID,
			Type:        v1.InteractionType_INTERACTION_TYPE_QUESTION,
			Title:       questionText,
			Description: description,
			Options:     interactionOpts,
		}))
		if err != nil {
			return nil, fmt.Errorf("failed to create question interaction: %w", err)
		}

		interactionID := resp.Msg.GetInteraction().GetId()
		log.Printf("[task:%s] AskUserQuestion: waiting for answer to question %d (interaction: %s)", taskID, i, interactionID)

		ch := waiter.Register(interactionID)

		var selectedAnswer string
		select {
		case <-ctx.Done():
			waiter.Unregister(interactionID)
			return claudeagent.PermissionResultDeny{Message: "context cancelled"}, nil
		case inter := <-ch:
			waiter.Unregister(interactionID)
			if inter.GetStatus() == v1.InteractionStatus_INTERACTION_STATUS_EXPIRED {
				return claudeagent.PermissionResultDeny{Message: "question expired"}, nil
			}
			selectedAnswer = inter.GetResponse()
		}

		// If user chose "Other", create a follow-up interaction for free-text input.
		if selectedAnswer == "__other__" {
			followResp, err := client.CreateInteraction(ctx, connect.NewRequest(&v1.CreateInteractionRequest{
				TaskId:      taskID,
				AgentId:     agentID,
				Type:        v1.InteractionType_INTERACTION_TYPE_QUESTION,
				Title:       questionText,
				Description: "Enter your custom answer:",
			}))
			if err != nil {
				return nil, fmt.Errorf("failed to create follow-up interaction: %w", err)
			}

			followID := followResp.Msg.GetInteraction().GetId()
			log.Printf("[task:%s] AskUserQuestion: waiting for free-text answer (interaction: %s)", taskID, followID)

			followCh := waiter.Register(followID)

			select {
			case <-ctx.Done():
				waiter.Unregister(followID)
				return claudeagent.PermissionResultDeny{Message: "context cancelled"}, nil
			case inter := <-followCh:
				waiter.Unregister(followID)
				if inter.GetStatus() == v1.InteractionStatus_INTERACTION_STATUS_EXPIRED {
					return claudeagent.PermissionResultDeny{Message: "question expired"}, nil
				}
				selectedAnswer = inter.GetResponse()
			}
		}

		answers[questionText] = selectedAnswer
		log.Printf("[task:%s] AskUserQuestion: question %d answered: %q", taskID, i, selectedAnswer)
	}

	// Inject answers into the input and return as UpdatedInput.
	updatedInput := make(map[string]any)
	for k, v := range input {
		updatedInput[k] = v
	}
	updatedInput["answers"] = answers

	return claudeagent.PermissionResultAllow{
		UpdatedInput: updatedInput,
	}, nil
}

func handlePermissionRequest(
	ctx context.Context,
	client taskguildv1connect.AgentManagerServiceClient,
	taskID string,
	agentID string,
	toolName string,
	input map[string]any,
	waiter *interactionWaiter,
	permMode claudeagent.PermissionMode,
	toolCtx claudeagent.ToolPermissionContext,
	permCache *permissionCache,
) (claudeagent.PermissionResult, error) {
	// bypassPermissions: allow everything
	if permMode == claudeagent.PermissionModeBypassPermissions {
		log.Printf("[task:%s] auto-allowing %s (bypassPermissions)", taskID, toolName)
		return claudeagent.PermissionResultAllow{}, nil
	}

	// Read-only tools: always allowed
	if readOnlyTools[toolName] {
		log.Printf("[task:%s] auto-allowing read-only tool %s", taskID, toolName)
		return claudeagent.PermissionResultAllow{}, nil
	}

	// Edit tools: allowed in acceptEdits mode
	if editTools[toolName] && permMode == claudeagent.PermissionModeAcceptEdits {
		log.Printf("[task:%s] auto-allowing edit tool %s (acceptEdits)", taskID, toolName)
		return claudeagent.PermissionResultAllow{}, nil
	}

	// AskUserQuestion: present as QUESTION interactions instead of permission request
	if toolName == "AskUserQuestion" {
		return handleAskUserQuestion(ctx, client, taskID, agentID, input, waiter)
	}

	// Check permission cache: auto-allow if a matching "always allow" rule exists.
	if permCache != nil && permCache.Check(toolName, input) {
		log.Printf("[task:%s] auto-allowing %s (permission cache hit)", taskID, toolName)
		return claudeagent.PermissionResultAllow{}, nil
	}

	description := formatToolDescription(toolName, input)

	resp, err := client.CreateInteraction(ctx, connect.NewRequest(&v1.CreateInteractionRequest{
		TaskId:      taskID,
		AgentId:     agentID,
		Type:        v1.InteractionType_INTERACTION_TYPE_PERMISSION_REQUEST,
		Title:       fmt.Sprintf("Permission request: %s", toolName),
		Description: description,
		Options: []*v1.InteractionOption{
			{Label: "Allow", Value: "allow", Description: "Allow this tool use"},
			{Label: "Always Allow", Value: "always_allow", Description: "Allow and remember the rule for future uses"},
			{Label: "Deny", Value: "deny", Description: "Deny this tool use"},
		},
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to create interaction: %w", err)
	}

	interactionID := resp.Msg.GetInteraction().GetId()
	log.Printf("[task:%s] waiting for permission response (interaction: %s, tool: %s)", taskID, interactionID, toolName)

	ch := waiter.Register(interactionID)
	defer waiter.Unregister(interactionID)

	select {
	case <-ctx.Done():
		return claudeagent.PermissionResultDeny{Message: "context cancelled"}, nil
	case inter := <-ch:
		if inter.GetStatus() == v1.InteractionStatus_INTERACTION_STATUS_EXPIRED {
			log.Printf("[task:%s] permission request expired for %s", taskID, toolName)
			return claudeagent.PermissionResultDeny{Message: "permission request expired"}, nil
		}
		switch inter.GetResponse() {
		case "allow":
			log.Printf("[task:%s] permission granted for %s", taskID, toolName)
			return claudeagent.PermissionResultAllow{}, nil
		case "always_allow":
			updates := buildPermissionUpdate(toolName, input, toolCtx.Suggestions)
			log.Printf("[task:%s] permission granted (always allow) for %s, updating %d rule(s)", taskID, toolName, len(updates))

			// Persist to cache and backend so future calls are auto-allowed.
			if permCache != nil {
				ruleStrings := extractRuleStrings(updates)
				go permCache.AddAndSync(ctx, ruleStrings)
			}

			return claudeagent.PermissionResultAllow{
				UpdatedPermissions: updates,
			}, nil
		default:
			log.Printf("[task:%s] permission denied for %s", taskID, toolName)
			return claudeagent.PermissionResultDeny{Message: "user denied permission"}, nil
		}
	}
}
