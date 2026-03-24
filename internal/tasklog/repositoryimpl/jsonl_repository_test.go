package repositoryimpl

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kazz187/taskguild/internal/tasklog"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/oklog/ulid/v2"
)

func createLog(projectID, taskID string, category int32, message string) *tasklog.TaskLog {
	return &tasklog.TaskLog{
		ID:        ulid.Make().String(),
		ProjectID: projectID,
		TaskID:    taskID,
		Level:     int32(taskguildv1.TaskLogLevel_TASK_LOG_LEVEL_INFO),
		Category:  category,
		Message:   message,
		CreatedAt: time.Now(),
	}
}

// TestMultiTurnCreateAndList verifies that logs from multiple turns are all
// returned by List with limit=0 (unlimited).
func TestMultiTurnCreateAndList(t *testing.T) {
	dir := t.TempDir()
	repo := NewJSONLRepository(dir)
	defer repo.Close()
	ctx := context.Background()

	const (
		projectID = "proj1"
		taskID    = "task1"
	)

	var allIDs []string

	// Turn 0: TURN_START, 3 entries, TURN_END
	log := createLog(projectID, taskID, int32(taskguildv1.TaskLogCategory_TASK_LOG_CATEGORY_TURN_START), "Turn 0 started")
	if err := repo.Create(ctx, log); err != nil {
		t.Fatalf("create TURN_START 0: %v", err)
	}
	allIDs = append(allIDs, log.ID)

	for i := 0; i < 3; i++ {
		log = createLog(projectID, taskID, int32(taskguildv1.TaskLogCategory_TASK_LOG_CATEGORY_TOOL_USE), fmt.Sprintf("Turn 0 tool %d", i))
		if err := repo.Create(ctx, log); err != nil {
			t.Fatalf("create tool_use turn 0: %v", err)
		}
		allIDs = append(allIDs, log.ID)
	}

	log = createLog(projectID, taskID, int32(taskguildv1.TaskLogCategory_TASK_LOG_CATEGORY_TURN_END), "Turn 0 ended")
	if err := repo.Create(ctx, log); err != nil {
		t.Fatalf("create TURN_END 0: %v", err)
	}
	allIDs = append(allIDs, log.ID)

	// Turn 1: TURN_START, 3 entries, TURN_END
	log = createLog(projectID, taskID, int32(taskguildv1.TaskLogCategory_TASK_LOG_CATEGORY_TURN_START), "Turn 1 started")
	if err := repo.Create(ctx, log); err != nil {
		t.Fatalf("create TURN_START 1: %v", err)
	}
	allIDs = append(allIDs, log.ID)

	for i := 0; i < 3; i++ {
		log = createLog(projectID, taskID, int32(taskguildv1.TaskLogCategory_TASK_LOG_CATEGORY_TOOL_USE), fmt.Sprintf("Turn 1 tool %d", i))
		if err := repo.Create(ctx, log); err != nil {
			t.Fatalf("create tool_use turn 1: %v", err)
		}
		allIDs = append(allIDs, log.ID)
	}

	log = createLog(projectID, taskID, int32(taskguildv1.TaskLogCategory_TASK_LOG_CATEGORY_TURN_END), "Turn 1 ended")
	if err := repo.Create(ctx, log); err != nil {
		t.Fatalf("create TURN_END 1: %v", err)
	}
	allIDs = append(allIDs, log.ID)

	// List with limit=0 (unlimited) — should return ALL entries from both turns.
	logs, total, err := repo.List(ctx, taskID, nil, 0, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if total != len(allIDs) {
		t.Errorf("total: got %d, want %d", total, len(allIDs))
	}
	if len(logs) != len(allIDs) {
		t.Errorf("len(logs): got %d, want %d", len(logs), len(allIDs))
	}

	// Verify all IDs are present.
	gotIDs := make(map[string]bool, len(logs))
	for _, l := range logs {
		gotIDs[l.ID] = true
	}
	for _, id := range allIDs {
		if !gotIDs[id] {
			t.Errorf("missing log ID %s", id)
		}
	}

	// Verify chronological order.
	for i := 1; i < len(logs); i++ {
		if logs[i].CreatedAt.Before(logs[i-1].CreatedAt) {
			t.Errorf("logs not in chronological order at index %d", i)
		}
	}
}

// TestMultiTurnPagination verifies that pagination correctly limits results
// while still having access to all entries.
func TestMultiTurnPagination(t *testing.T) {
	dir := t.TempDir()
	repo := NewJSONLRepository(dir)
	defer repo.Close()
	ctx := context.Background()

	const (
		projectID = "proj1"
		taskID    = "task1"
	)

	totalEntries := 0

	// Create 2 turns with 5 entries each (including TURN_START/END).
	for turn := 0; turn < 2; turn++ {
		log := createLog(projectID, taskID, int32(taskguildv1.TaskLogCategory_TASK_LOG_CATEGORY_TURN_START), fmt.Sprintf("Turn %d started", turn))
		if err := repo.Create(ctx, log); err != nil {
			t.Fatal(err)
		}
		totalEntries++

		for i := 0; i < 3; i++ {
			log = createLog(projectID, taskID, int32(taskguildv1.TaskLogCategory_TASK_LOG_CATEGORY_TOOL_USE), fmt.Sprintf("Turn %d tool %d", turn, i))
			if err := repo.Create(ctx, log); err != nil {
				t.Fatal(err)
			}
			totalEntries++
		}

		log = createLog(projectID, taskID, int32(taskguildv1.TaskLogCategory_TASK_LOG_CATEGORY_TURN_END), fmt.Sprintf("Turn %d ended", turn))
		if err := repo.Create(ctx, log); err != nil {
			t.Fatal(err)
		}
		totalEntries++
	}

	// Paginate with limit=3.
	logs, total, err := repo.List(ctx, taskID, nil, 3, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != totalEntries {
		t.Errorf("total: got %d, want %d", total, totalEntries)
	}
	if len(logs) != 3 {
		t.Errorf("len(logs): got %d, want 3", len(logs))
	}

	// Second page.
	logs2, total2, err := repo.List(ctx, taskID, nil, 3, 3)
	if err != nil {
		t.Fatalf("List page 2: %v", err)
	}
	if total2 != totalEntries {
		t.Errorf("total page 2: got %d, want %d", total2, totalEntries)
	}
	if len(logs2) != 3 {
		t.Errorf("len(logs2): got %d, want 3", len(logs2))
	}

	// Ensure no overlap.
	for _, l1 := range logs {
		for _, l2 := range logs2 {
			if l1.ID == l2.ID {
				t.Errorf("duplicate ID across pages: %s", l1.ID)
			}
		}
	}
}

// TestMultiTurnServerRestart verifies that a new repository instance
// recovers all entries from disk via ensureIndex.
func TestMultiTurnServerRestart(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	const (
		projectID = "proj1"
		taskID    = "task1"
	)

	var allIDs []string

	// Phase 1: Create logs with first repo instance.
	{
		repo := NewJSONLRepository(dir)
		for turn := 0; turn < 2; turn++ {
			log := createLog(projectID, taskID, int32(taskguildv1.TaskLogCategory_TASK_LOG_CATEGORY_TURN_START), fmt.Sprintf("Turn %d started", turn))
			if err := repo.Create(ctx, log); err != nil {
				t.Fatal(err)
			}
			allIDs = append(allIDs, log.ID)

			log = createLog(projectID, taskID, int32(taskguildv1.TaskLogCategory_TASK_LOG_CATEGORY_AGENT_OUTPUT), fmt.Sprintf("Turn %d output", turn))
			if err := repo.Create(ctx, log); err != nil {
				t.Fatal(err)
			}
			allIDs = append(allIDs, log.ID)

			log = createLog(projectID, taskID, int32(taskguildv1.TaskLogCategory_TASK_LOG_CATEGORY_TURN_END), fmt.Sprintf("Turn %d ended", turn))
			if err := repo.Create(ctx, log); err != nil {
				t.Fatal(err)
			}
			allIDs = append(allIDs, log.ID)
		}
		repo.Close()
	}

	// Phase 2: Create a NEW repo instance (simulating server restart).
	{
		repo := NewJSONLRepository(dir)
		defer repo.Close()

		logs, total, err := repo.List(ctx, taskID, nil, 0, 0)
		if err != nil {
			t.Fatalf("List after restart: %v", err)
		}
		if total != len(allIDs) {
			t.Errorf("total after restart: got %d, want %d", total, len(allIDs))
		}
		if len(logs) != len(allIDs) {
			t.Errorf("len(logs) after restart: got %d, want %d", len(logs), len(allIDs))
		}

		gotIDs := make(map[string]bool, len(logs))
		for _, l := range logs {
			gotIDs[l.ID] = true
		}
		for _, id := range allIDs {
			if !gotIDs[id] {
				t.Errorf("missing log ID after restart: %s", id)
			}
		}
	}
}
