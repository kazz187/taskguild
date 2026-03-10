package main

import (
	"strings"
	"testing"
)

func TestBuildWorkflowContext(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]string
		contains []string
		excludes []string
		empty    bool
	}{
		{
			name:     "empty metadata",
			metadata: map[string]string{},
			empty:    true,
		},
		{
			name:     "no workflow ID",
			metadata: map[string]string{"_task_title": "Test"},
			empty:    true,
		},
		{
			name: "full metadata with hooks",
			metadata: map[string]string{
				"_workflow_id":           "wf1",
				"_current_status_id":    "s2",
				"_current_status_name":  "In Progress",
				"_workflow_statuses":    `[{"id":"s1","name":"Backlog"},{"id":"s2","name":"In Progress"},{"id":"s3","name":"Review"},{"id":"s4","name":"Done"}]`,
				"_available_transitions": `[{"id":"s3","name":"Review"}]`,
				"_hooks":                `[{"name":"Run linter","action_type":"skill","trigger":"before_task_execution"},{"name":"Deploy check","action_type":"script","trigger":"after_task_execution"}]`,
			},
			contains: []string{
				"## TaskGuild Workflow Context",
				"### Workflow Statuses",
				"- s1: Backlog\n",
				"- s2: In Progress  <-- current\n",
				"- s3: Review\n",
				"- s4: Done\n",
				"### Available Transitions",
				"- s3: Review\n",
				"### Hooks",
				`"Run linter" (skill)`,
				"before_task_execution",
				`"Deploy check" (script)`,
				"after_task_execution",
			},
		},
		{
			name: "full metadata without hooks",
			metadata: map[string]string{
				"_workflow_id":           "wf1",
				"_current_status_id":    "s1",
				"_current_status_name":  "Backlog",
				"_workflow_statuses":    `[{"id":"s1","name":"Backlog"},{"id":"s2","name":"In Progress"}]`,
				"_available_transitions": `[{"id":"s2","name":"In Progress"}]`,
			},
			contains: []string{
				"### Workflow Statuses",
				"- s1: Backlog  <-- current\n",
				"- s2: In Progress\n",
				"### Available Transitions",
			},
			excludes: []string{
				"### Hooks",
			},
		},
		{
			name: "malformed JSON graceful handling",
			metadata: map[string]string{
				"_workflow_id":           "wf1",
				"_workflow_statuses":    `invalid json`,
				"_available_transitions": `also invalid`,
				"_hooks":                `not json`,
			},
			contains: []string{
				"## TaskGuild Workflow Context",
			},
			excludes: []string{
				"### Workflow Statuses",
				"### Available Transitions",
				"### Hooks",
			},
		},
		{
			name: "empty hooks array omits section",
			metadata: map[string]string{
				"_workflow_id":        "wf1",
				"_workflow_statuses": `[{"id":"s1","name":"Draft"}]`,
				"_hooks":             `[]`,
			},
			excludes: []string{
				"### Hooks",
			},
		},
		{
			name: "current status marker on correct status",
			metadata: map[string]string{
				"_workflow_id":        "wf1",
				"_current_status_id": "s3",
				"_workflow_statuses": `[{"id":"s1","name":"A"},{"id":"s2","name":"B"},{"id":"s3","name":"C"}]`,
			},
			contains: []string{
				"- s1: A\n",
				"- s2: B\n",
				"- s3: C  <-- current\n",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildWorkflowContext(tt.metadata)

			if tt.empty {
				if result != "" {
					t.Errorf("expected empty string, got:\n%s", result)
				}
				return
			}

			for _, s := range tt.contains {
				if !strings.Contains(result, s) {
					t.Errorf("expected result to contain %q, got:\n%s", s, result)
				}
			}

			for _, s := range tt.excludes {
				if strings.Contains(result, s) {
					t.Errorf("expected result NOT to contain %q, got:\n%s", s, result)
				}
			}
		})
	}
}
