package agentmanager

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"

	"github.com/kazz187/taskguild/internal/script"
	"github.com/kazz187/taskguild/pkg/cerr"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
)

// --- Script comparison & conflict resolution RPCs ---

// RequestScriptComparison sends a CompareScriptsCommand to connected agent-managers
// so they compare local scripts with server versions.
func (s *Server) RequestScriptComparison(ctx context.Context, req *connect.Request[taskguildv1.RequestScriptComparisonRequest]) (*connect.Response[taskguildv1.RequestScriptComparisonResponse], error) {
	if req.Msg.ProjectId == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_id is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.Get(ctx, req.Msg.ProjectId)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	// Fetch all scripts for this project so the agent can compare.
	scripts, _, err := s.scriptRepo.List(ctx, proj.ID, 1000, 0)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	protos := make([]*taskguildv1.ScriptDefinition, len(scripts))
	for i, sc := range scripts {
		protos[i] = scriptToProto(sc)
	}

	requestID := ulid.Make().String()

	s.registry.BroadcastCommandToProject(proj.Name, &taskguildv1.AgentCommand{
		Command: &taskguildv1.AgentCommand_CompareScripts{
			CompareScripts: &taskguildv1.CompareScriptsCommand{
				RequestId: requestID,
				Scripts:   protos,
			},
		},
	})

	slog.Info("script comparison requested",
		"project_id", req.Msg.ProjectId,
		"project_name", proj.Name,
		"request_id", requestID,
		"script_count", len(scripts),
	)

	return connect.NewResponse(&taskguildv1.RequestScriptComparisonResponse{
		RequestId: requestID,
	}), nil
}

// ReportScriptComparison receives comparison results from the agent and caches them.
func (s *Server) ReportScriptComparison(ctx context.Context, req *connect.Request[taskguildv1.ReportScriptComparisonRequest]) (*connect.Response[taskguildv1.ReportScriptComparisonResponse], error) {
	projectName := req.Msg.ProjectName
	if projectName == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_name is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.FindByName(ctx, projectName)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	// Cache the diffs for this project.
	s.scriptDiffMu.Lock()
	s.scriptDiffCache[proj.ID] = req.Msg.Diffs
	s.scriptDiffMu.Unlock()

	// Publish event so frontend can pick up the comparison results.
	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_SCRIPT_COMPARISON,
		req.Msg.RequestId,
		"",
		map[string]string{
			"project_id": proj.ID,
			"request_id": req.Msg.RequestId,
			"diff_count": strconv.Itoa(len(req.Msg.Diffs)),
		},
	)

	slog.Info("script comparison reported",
		"project_id", proj.ID,
		"project_name", projectName,
		"request_id", req.Msg.RequestId,
		"diff_count", len(req.Msg.Diffs),
	)

	return connect.NewResponse(&taskguildv1.ReportScriptComparisonResponse{}), nil
}

// GetScriptComparison returns the cached script diffs for a project.
func (s *Server) GetScriptComparison(ctx context.Context, req *connect.Request[taskguildv1.GetScriptComparisonRequest]) (*connect.Response[taskguildv1.GetScriptComparisonResponse], error) {
	if req.Msg.ProjectId == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_id is required", nil).ConnectError()
	}

	s.scriptDiffMu.RLock()
	diffs := s.scriptDiffCache[req.Msg.ProjectId]
	s.scriptDiffMu.RUnlock()

	return connect.NewResponse(&taskguildv1.GetScriptComparisonResponse{
		Diffs: diffs,
	}), nil
}

// ResolveScriptConflict resolves a single script conflict between server and agent versions.
func (s *Server) ResolveScriptConflict(ctx context.Context, req *connect.Request[taskguildv1.ResolveScriptConflictRequest]) (*connect.Response[taskguildv1.ResolveScriptConflictResponse], error) {
	if req.Msg.ProjectId == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_id is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.Get(ctx, req.Msg.ProjectId)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	var resultScript *script.Script

	switch req.Msg.Choice {
	case taskguildv1.ScriptResolutionChoice_SCRIPT_RESOLUTION_CHOICE_SERVER:
		// Server version wins. DB is already correct.
		// Force-overwrite the agent's local file by sending SyncScriptsCommand.
		if req.Msg.ScriptId != "" {
			resultScript, err = s.scriptRepo.Get(ctx, req.Msg.ScriptId)
			if err != nil {
				return nil, cerr.ExtractConnectError(ctx, err)
			}

			s.registry.BroadcastCommandToProject(proj.Name, &taskguildv1.AgentCommand{
				Command: &taskguildv1.AgentCommand_SyncScripts{
					SyncScripts: &taskguildv1.SyncScriptsCommand{
						ForceOverwriteScriptIds: []string{req.Msg.ScriptId},
					},
				},
			})
		}

	case taskguildv1.ScriptResolutionChoice_SCRIPT_RESOLUTION_CHOICE_AGENT:
		// Agent version wins. Update the DB with agent's content.
		if req.Msg.ScriptId != "" {
			// Update existing script.
			resultScript, err = s.scriptRepo.Get(ctx, req.Msg.ScriptId)
			if err != nil {
				return nil, cerr.ExtractConnectError(ctx, err)
			}
			resultScript.Content = req.Msg.AgentContent
			if req.Msg.Filename != "" {
				resultScript.Filename = req.Msg.Filename
			}
			resultScript.IsSynced = true
			resultScript.UpdatedAt = time.Now()
			if err := s.scriptRepo.Update(ctx, resultScript); err != nil {
				return nil, cerr.ExtractConnectError(ctx, err)
			}
		} else {
			// Agent-only script — create new in DB.
			now := time.Now()
			filename := req.Msg.Filename
			if filename == "" {
				filename = req.Msg.ScriptName + ".sh"
			}
			resultScript = &script.Script{
				ID:        ulid.Make().String(),
				ProjectID: req.Msg.ProjectId,
				Name:      req.Msg.ScriptName,
				Filename:  filename,
				Content:   req.Msg.AgentContent,
				IsSynced:  true,
				CreatedAt: now,
				UpdatedAt: now,
			}
			if err := s.scriptRepo.Create(ctx, resultScript); err != nil {
				return nil, cerr.ExtractConnectError(ctx, err)
			}
		}

	default:
		return nil, cerr.NewError(cerr.InvalidArgument, "invalid resolution choice", nil).ConnectError()
	}

	// Remove the resolved diff from cache.
	s.removeScriptDiff(req.Msg.ProjectId, req.Msg.ScriptId, req.Msg.Filename)

	var proto *taskguildv1.ScriptDefinition
	if resultScript != nil {
		proto = scriptToProto(resultScript)
	}

	slog.Info("script conflict resolved",
		"project_id", req.Msg.ProjectId,
		"script_id", req.Msg.ScriptId,
		"script_name", req.Msg.ScriptName,
		"choice", req.Msg.Choice.String(),
	)

	return connect.NewResponse(&taskguildv1.ResolveScriptConflictResponse{
		Script: proto,
	}), nil
}

// removeScriptDiff removes a specific diff entry from the cache.
// It matches by script_id if non-empty, otherwise by filename.
func (s *Server) removeScriptDiff(projectID, scriptID, filename string) {
	s.scriptDiffMu.Lock()
	defer s.scriptDiffMu.Unlock()

	diffs := s.scriptDiffCache[projectID]
	if len(diffs) == 0 {
		return
	}

	filtered := make([]*taskguildv1.ScriptDiff, 0, len(diffs))
	for _, d := range diffs {
		if scriptID != "" && d.ScriptId == scriptID {
			continue // remove this diff
		}
		if scriptID == "" && filename != "" && d.Filename == filename {
			continue // remove agent-only diff by filename
		}
		filtered = append(filtered, d)
	}
	s.scriptDiffCache[projectID] = filtered
}
