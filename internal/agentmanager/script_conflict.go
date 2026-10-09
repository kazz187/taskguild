package agentmanager

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/kazz187/taskguild/internal/eventbus"
	"github.com/kazz187/taskguild/internal/script"
	"github.com/kazz187/taskguild/pkg/cerr"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
)

// --- Script comparison & conflict resolution RPCs ---

// RequestScriptComparison sends a CompareScriptsCommand to connected agent-managers
// so they compare local scripts with server versions.
func (s *Server) RequestScriptComparison(ctx context.Context, req *taskguildv1.RequestScriptComparisonRequest) (*taskguildv1.RequestScriptComparisonResponse, error) {
	if req.GetProjectId() == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_id is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.Get(ctx, req.GetProjectId())
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
		"project_id", req.GetProjectId(),
		"project_name", proj.Name,
		"request_id", requestID,
		"script_count", len(scripts),
	)

	return &taskguildv1.RequestScriptComparisonResponse{
		RequestId: requestID,
	}, nil
}

// ReportScriptComparison receives comparison results from the agent and caches them.
func (s *Server) ReportScriptComparison(ctx context.Context, req *taskguildv1.ReportScriptComparisonRequest) (*taskguildv1.ReportScriptComparisonResponse, error) {
	projectName := req.GetProjectName()
	if projectName == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_name is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.FindByName(ctx, projectName)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	// Cache the diffs for this project.
	s.scriptDiffMu.Lock()
	s.scriptDiffCache[proj.ID] = req.GetDiffs()
	s.scriptDiffMu.Unlock()

	// Publish event so frontend can pick up the comparison results.
	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_SCRIPT_COMPARISON,
		req.GetRequestId(),
		"",
		map[string]string{
			eventbus.MetaProjectID: proj.ID,
			eventbus.MetaRequestID: req.GetRequestId(),
			eventbus.MetaDiffCount: strconv.Itoa(len(req.GetDiffs())),
		},
	)

	slog.Info("script comparison reported",
		"project_id", proj.ID,
		"project_name", projectName,
		"request_id", req.GetRequestId(),
		"diff_count", len(req.GetDiffs()),
	)

	return &taskguildv1.ReportScriptComparisonResponse{}, nil
}

// GetScriptComparison returns the cached script diffs for a project.
func (s *Server) GetScriptComparison(ctx context.Context, req *taskguildv1.GetScriptComparisonRequest) (*taskguildv1.GetScriptComparisonResponse, error) {
	if req.GetProjectId() == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_id is required", nil).ConnectError()
	}

	s.scriptDiffMu.RLock()
	diffs := s.scriptDiffCache[req.GetProjectId()]
	s.scriptDiffMu.RUnlock()

	return &taskguildv1.GetScriptComparisonResponse{
		Diffs: diffs,
	}, nil
}

// ResolveScriptConflict resolves a single script conflict between server and agent versions.
func (s *Server) ResolveScriptConflict(ctx context.Context, req *taskguildv1.ResolveScriptConflictRequest) (*taskguildv1.ResolveScriptConflictResponse, error) {
	if req.GetProjectId() == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_id is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.Get(ctx, req.GetProjectId())
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	var resultScript *script.Script

	switch req.GetChoice() {
	case taskguildv1.ScriptResolutionChoice_SCRIPT_RESOLUTION_CHOICE_SERVER:
		// Server version wins. DB is already correct.
		// Force-overwrite the agent's local file by sending SyncScriptsCommand.
		if req.GetScriptId() != "" {
			resultScript, err = s.scriptRepo.Get(ctx, req.GetScriptId())
			if err != nil {
				return nil, cerr.ExtractConnectError(ctx, err)
			}

			s.registry.BroadcastCommandToProject(proj.Name, &taskguildv1.AgentCommand{
				Command: &taskguildv1.AgentCommand_SyncScripts{
					SyncScripts: &taskguildv1.SyncScriptsCommand{
						ForceOverwriteScriptIds: []string{req.GetScriptId()},
					},
				},
			})
		}

	case taskguildv1.ScriptResolutionChoice_SCRIPT_RESOLUTION_CHOICE_AGENT:
		// Agent version wins. Update the DB with agent's content.
		if req.GetScriptId() != "" {
			// Update existing script.
			resultScript, err = s.scriptRepo.Get(ctx, req.GetScriptId())
			if err != nil {
				return nil, cerr.ExtractConnectError(ctx, err)
			}

			resultScript.Content = req.GetAgentContent()
			if req.GetFilename() != "" {
				resultScript.Filename = req.GetFilename()
			}

			resultScript.IsSynced = true

			resultScript.UpdatedAt = time.Now()

			err := s.scriptRepo.Update(ctx, resultScript)
			if err != nil {
				return nil, cerr.ExtractConnectError(ctx, err)
			}
		} else {
			// Agent-only script — create new in DB.
			now := time.Now()

			filename := req.GetFilename()
			if filename == "" {
				filename = req.GetScriptName() + ".sh"
			}

			resultScript = &script.Script{
				ID:        ulid.Make().String(),
				ProjectID: req.GetProjectId(),
				Name:      req.GetScriptName(),
				Filename:  filename,
				Content:   req.GetAgentContent(),
				IsSynced:  true,
				CreatedAt: now,
				UpdatedAt: now,
			}

			err := s.scriptRepo.Create(ctx, resultScript)
			if err != nil {
				return nil, cerr.ExtractConnectError(ctx, err)
			}
		}

	default:
		return nil, cerr.NewError(cerr.InvalidArgument, "invalid resolution choice", nil).ConnectError()
	}

	// Remove the resolved diff from cache.
	s.removeScriptDiff(req.GetProjectId(), req.GetScriptId(), req.GetFilename())

	var proto *taskguildv1.ScriptDefinition
	if resultScript != nil {
		proto = scriptToProto(resultScript)
	}

	slog.Info("script conflict resolved",
		"project_id", req.GetProjectId(),
		"script_id", req.GetScriptId(),
		"script_name", req.GetScriptName(),
		"choice", req.GetChoice().String(),
	)

	return &taskguildv1.ResolveScriptConflictResponse{
		Script: proto,
	}, nil
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
		if scriptID != "" && d.GetScriptId() == scriptID {
			continue // remove this diff
		}

		if scriptID == "" && filename != "" && d.GetFilename() == filename {
			continue // remove agent-only diff by filename
		}

		filtered = append(filtered, d)
	}

	s.scriptDiffCache[projectID] = filtered
}
