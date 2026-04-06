package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"connectrpc.com/connect"
	claudeagent "github.com/kazz187/claude-agent-sdk-go"
	v1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
	"github.com/kazz187/taskguild/pkg/clog"
	"github.com/kazz187/taskguild/pkg/shellparse"
)

// interactionWaiter dispatches streamed interaction responses to waiting goroutines.
// It buffers responses that arrive before a waiter registers (race condition between
// CreateInteraction and Register).
type interactionWaiter struct {
	mu        sync.Mutex
	waiters   map[string]chan *v1.Interaction // interaction_id -> ch
	pending   map[string]*v1.Interaction      // arrived before Register()
	userMsgCh chan *v1.Interaction             // task-level channel for user-initiated messages
}

func newInteractionWaiter() *interactionWaiter {
	return &interactionWaiter{
		waiters:   make(map[string]chan *v1.Interaction),
		pending:   make(map[string]*v1.Interaction),
		userMsgCh: make(chan *v1.Interaction, 16),
	}
}

// DeliverUserMessage sends a user-initiated message to the task-level channel.
// Non-blocking: if the channel is full the message is dropped (logged by caller).
func (w *interactionWaiter) DeliverUserMessage(inter *v1.Interaction) {
	select {
	case w.userMsgCh <- inter:
	default:
	}
}

// UserMessages returns the receive-only channel for user-initiated messages.
func (w *interactionWaiter) UserMessages() <-chan *v1.Interaction {
	return w.userMsgCh
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
// responded interactions to the waiter. It automatically reconnects on stream
// errors with exponential backoff.
// It returns only when ctx is cancelled (task finished).
func runInteractionListener(ctx context.Context, interClient taskguildv1connect.InteractionServiceClient, taskID string, waiter *interactionWaiter) {
	logger := clog.LoggerFromContext(ctx)

	backoff := 1 * time.Second
	const maxBackoff = 30 * time.Second

	for {
		if ctx.Err() != nil {
			return
		}

		err := runInteractionStream(ctx, interClient, taskID, waiter)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			logger.Warn("interaction stream error, reconnecting", "error", err, "backoff", backoff)
		} else {
			logger.Info("interaction stream ended, reconnecting", "backoff", backoff)
		}

		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return
		}

		// Exponential backoff capped at maxBackoff, reset on successful connection.
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// runInteractionStream connects once and processes interaction events until
// the stream ends or ctx is cancelled. Returns the stream error (if any).
func runInteractionStream(ctx context.Context, interClient taskguildv1connect.InteractionServiceClient, taskID string, waiter *interactionWaiter) error {
	logger := clog.LoggerFromContext(ctx)

	stream, err := interClient.SubscribeInteractions(ctx, connect.NewRequest(&v1.SubscribeInteractionsRequest{
		TaskId: taskID,
	}))
	if err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}
	defer stream.Close()

	logger.Debug("interaction stream connected")

	for stream.Receive() {
		deliverInteraction(taskID, stream.Msg().GetInteraction(), waiter, "stream")
	}

	if err := stream.Err(); err != nil && ctx.Err() == nil {
		return fmt.Errorf("stream error: %w", err)
	}
	return nil
}

// deliverInteraction checks the interaction status and delivers responded/expired
// interactions to the waiter.
func deliverInteraction(taskID string, inter *v1.Interaction, waiter *interactionWaiter, source string) {
	if inter == nil {
		return
	}
	logger := slog.Default().With("task_id", taskID)

	// User-initiated messages (sent via "Send a message") are routed to
	// the task-level channel instead of the ID-based waiter.
	if inter.GetType() == v1.InteractionType_INTERACTION_TYPE_USER_MESSAGE {
		logger.Debug("user message received", "interaction_id", inter.GetId(), "source", source)
		waiter.DeliverUserMessage(inter)
		return
	}

	switch inter.GetStatus() {
	case v1.InteractionStatus_INTERACTION_STATUS_RESPONDED:
		logger.Debug("interaction responded", "interaction_id", inter.GetId(), "source", source)
		waiter.Deliver(inter)
	case v1.InteractionStatus_INTERACTION_STATUS_EXPIRED:
		logger.Debug("interaction expired", "interaction_id", inter.GetId(), "source", source)
		waiter.Deliver(inter)
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
	logger := clog.LoggerFromContext(ctx)

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
	logger.Info("waiting for user response", "interaction_id", interactionID)

	ch := waiter.Register(interactionID)
	defer waiter.Unregister(interactionID)

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(waitForUserResponseTimeout):
		logger.Warn("user response timeout, expiring interaction", "interaction_id", interactionID)
		// Expire the pending interaction so it disappears from the UI.
		if _, expErr := interClient.ExpireInteraction(ctx, connect.NewRequest(&v1.ExpireInteractionRequest{
			Id: interactionID,
		})); expErr != nil {
			logger.Error("failed to expire interaction", "interaction_id", interactionID, "error", expErr)
		}
		return "", errWaitTimeout
	case inter := <-ch:
		if inter.GetStatus() == v1.InteractionStatus_INTERACTION_STATUS_EXPIRED {
			return "", errWaitTimeout
		}
		logger.Info("user responded to interaction", "interaction_id", interactionID)
		return inter.GetResponse(), nil
	case msg := <-waiter.UserMessages():
		// User sent a free-form message via "Send a message" — use it as
		// the response and expire the pending QUESTION interaction.
		logger.Info("user sent message while waiting for input", "interaction_id", interactionID, "message_id", msg.GetId())
		if _, expErr := interClient.ExpireInteraction(ctx, connect.NewRequest(&v1.ExpireInteractionRequest{
			Id: interactionID,
		})); expErr != nil {
			logger.Error("failed to expire interaction", "interaction_id", interactionID, "error", expErr)
		}
		return msg.GetTitle(), nil
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
	logger := clog.LoggerFromContext(ctx)

	questionsRaw, ok := input["questions"]
	if !ok {
		logger.Warn("AskUserQuestion: no questions field")
		return claudeagent.PermissionResultAllow{}, nil
	}

	questionsSlice, ok := questionsRaw.([]any)
	if !ok {
		logger.Warn("AskUserQuestion: questions is not an array")
		return claudeagent.PermissionResultAllow{}, nil
	}

	answers := make(map[string]any)

	for i, qRaw := range questionsSlice {
		qMap, ok := qRaw.(map[string]any)
		if !ok {
			logger.Warn("AskUserQuestion: question is not a map", "index", i)
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
		logger.Info("AskUserQuestion: waiting for answer", "question_index", i, "interaction_id", interactionID)

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
			logger.Info("AskUserQuestion: waiting for free-text answer", "interaction_id", followID)

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
		logger.Debug("AskUserQuestion: question answered", "question_index", i, "answer", selectedAnswer)
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
	scpCache *singleCommandPermissionCache,
) (claudeagent.PermissionResult, error) {
	logger := clog.LoggerFromContext(ctx)

	// bypassPermissions: allow everything
	if permMode == claudeagent.PermissionModeBypassPermissions {
		logger.Debug("auto-allowing tool (bypassPermissions)", "tool", toolName)
		return claudeagent.PermissionResultAllow{}, nil
	}

	// Read-only tools: always allowed
	if readOnlyTools[toolName] {
		logger.Debug("auto-allowing read-only tool", "tool", toolName)
		return claudeagent.PermissionResultAllow{}, nil
	}

	// Edit tools: allowed in acceptEdits mode
	if editTools[toolName] && permMode == claudeagent.PermissionModeAcceptEdits {
		logger.Debug("auto-allowing edit tool (acceptEdits)", "tool", toolName)
		return claudeagent.PermissionResultAllow{}, nil
	}

	// AskUserQuestion: present as QUESTION interactions instead of permission request
	if toolName == "AskUserQuestion" {
		return handleAskUserQuestion(ctx, client, taskID, agentID, input, waiter)
	}

	// Check permission cache: auto-allow if a matching "always allow" rule exists.
	if permCache != nil && permCache.Check(toolName, input) {
		logger.Debug("auto-allowing tool (permission cache hit)", "tool", toolName)
		return claudeagent.PermissionResultAllow{}, nil
	}

	// Single-command permission check for Bash tool.
	var bashMeta *bashPermissionMetadata
	if toolName == "Bash" && scpCache != nil {
		if cmdRaw, ok := input["command"]; ok {
			if cmdStr, ok := cmdRaw.(string); ok && cmdStr != "" {
				parsed := shellparse.Parse(cmdStr)
				allMatched, meta := scpCache.CheckAllCommands(parsed)
				if allMatched {
					logger.Debug("auto-allowing Bash (all commands matched)", "command", cmdStr)
					return claudeagent.PermissionResultAllow{}, nil
				}
				bashMeta = meta
			}
		}
	}

	description := formatToolDescription(toolName, input)

	// Build interaction options based on tool type.
	var options []*v1.InteractionOption
	if toolName == "Bash" {
		options = []*v1.InteractionOption{
			{Label: "Allow", Value: "allow", Description: "Allow this tool use"},
			{Label: "Always Allow Command", Value: "always_allow_command", Description: "Allow and create rules for individual commands"},
			{Label: "Deny", Value: "deny", Description: "Deny this tool use"},
		}
	} else {
		options = []*v1.InteractionOption{
			{Label: "Allow", Value: "allow", Description: "Allow this tool use"},
			{Label: "Deny", Value: "deny", Description: "Deny this tool use"},
		}
	}

	// Attach parsed command metadata for Bash interactions.
	var metadataJSON string
	if bashMeta != nil {
		if data, err := json.Marshal(bashMeta); err == nil {
			metadataJSON = string(data)
		}
	}

	resp, err := client.CreateInteraction(ctx, connect.NewRequest(&v1.CreateInteractionRequest{
		TaskId:      taskID,
		AgentId:     agentID,
		Type:        v1.InteractionType_INTERACTION_TYPE_PERMISSION_REQUEST,
		Title:       fmt.Sprintf("Permission request: %s", toolName),
		Description: description,
		Options:     options,
		Metadata:    metadataJSON,
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to create interaction: %w", err)
	}

	interactionID := resp.Msg.GetInteraction().GetId()
	logger.Info("waiting for permission response", "interaction_id", interactionID, "tool", toolName)

	ch := waiter.Register(interactionID)
	defer waiter.Unregister(interactionID)

	select {
	case <-ctx.Done():
		return claudeagent.PermissionResultDeny{Message: "context cancelled"}, nil
	case inter := <-ch:
		if inter.GetStatus() == v1.InteractionStatus_INTERACTION_STATUS_EXPIRED {
			logger.Info("permission request expired", "tool", toolName)
			return claudeagent.PermissionResultDeny{Message: "permission request expired"}, nil
		}

		responseStr := inter.GetResponse()

		// Try to parse the response as JSON (always_allow_command from frontend).
		var aacResp alwaysAllowCommandResponse
		if json.Unmarshal([]byte(responseStr), &aacResp) == nil && aacResp.Action == "always_allow_command" {
			return handleAlwaysAllowCommand(ctx, client, scpCache, aacResp.Rules, toolName, logger)
		}

		switch responseStr {
		case "allow":
			logger.Info("permission granted", "tool", toolName)
			return claudeagent.PermissionResultAllow{}, nil
		default:
			logger.Info("permission denied", "tool", toolName)
			return claudeagent.PermissionResultDeny{Message: "user denied permission"}, nil
		}
	}
}

// alwaysAllowCommandResponse is the JSON structure sent by the frontend when
// the user clicks "Always Allow Command".
type alwaysAllowCommandResponse struct {
	Action string                           `json:"action"`
	Rules  []alwaysAllowCommandResponseRule `json:"rules"`
}

type alwaysAllowCommandResponseRule struct {
	Pattern string `json:"pattern"`
	Type    string `json:"type"`
}

// handleAlwaysAllowCommand processes the "always_allow_command" response by
// registering the user-edited wildcard rules via the AddSingleCommandPermission RPC.
func handleAlwaysAllowCommand(
	ctx context.Context,
	client taskguildv1connect.AgentManagerServiceClient,
	scpCache *singleCommandPermissionCache,
	rules []alwaysAllowCommandResponseRule,
	toolName string,
	logger *slog.Logger,
) (claudeagent.PermissionResult, error) {
	var registered int
	for _, rule := range rules {
		if rule.Pattern == "" {
			continue
		}
		ruleType := rule.Type
		if ruleType == "" {
			ruleType = "command"
		}
		_, err := client.AddSingleCommandPermission(ctx, connect.NewRequest(&v1.AddSingleCommandPermissionRequest{
			ProjectName: scpCache.projectName,
			Pattern:     rule.Pattern,
			Type:        ruleType,
		}))
		if err != nil {
			logger.Error("failed to add single command permission", "pattern", rule.Pattern, "error", err)
			continue
		}
		registered++
	}

	logger.Info("permission granted (always allow command)", "tool", toolName, "rules_registered", registered)

	// Immediately refresh the cache so subsequent calls are auto-allowed.
	if scpCache != nil {
		go scpCache.Sync(ctx)
	}

	return claudeagent.PermissionResultAllow{}, nil
}
