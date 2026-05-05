package agentmanager

import (
	"testing"

	"github.com/kazz187/taskguild/internal/task"
	"github.com/kazz187/taskguild/internal/workflow"
)

func TestResolveEffort(t *testing.T) {
	tests := []struct {
		name         string
		taskEffort   string
		statusEffort string
		nilStatus    bool
		expected     string
	}{
		{
			name:         "task effort overrides status effort",
			taskEffort:   "low",
			statusEffort: "high",
			expected:     "low",
		},
		{
			name:         "empty task effort falls back to status effort",
			taskEffort:   "",
			statusEffort: "high",
			expected:     "high",
		},
		{
			name:         "both empty returns empty",
			taskEffort:   "",
			statusEffort: "",
			expected:     "",
		},
		{
			name:       "nil status uses task effort",
			taskEffort: "max",
			nilStatus:  true,
			expected:   "max",
		},
		{
			name:       "nil status and empty task returns empty",
			taskEffort: "",
			nilStatus:  true,
			expected:   "",
		},
		{
			name:         "non-empty task effort overrides empty status effort",
			taskEffort:   "medium",
			statusEffort: "",
			expected:     "medium",
		},
		{
			name:         "task xhigh effort overrides status max effort",
			taskEffort:   "xhigh",
			statusEffort: "max",
			expected:     "xhigh",
		},
		{
			name:         "status xhigh propagates when task effort empty",
			taskEffort:   "",
			statusEffort: "xhigh",
			expected:     "xhigh",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			taskObj := &task.Task{Effort: tc.taskEffort}

			var status *workflow.Status
			if !tc.nilStatus {
				status = &workflow.Status{Effort: tc.statusEffort}
			}

			got := resolveEffort(taskObj, status)
			if got != tc.expected {
				t.Errorf("resolveEffort(%q, status.effort=%q nilStatus=%v) = %q, want %q",
					tc.taskEffort, tc.statusEffort, tc.nilStatus, got, tc.expected)
			}
		})
	}
}
