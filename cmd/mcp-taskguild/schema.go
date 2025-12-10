package main

import (
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/jsonschema"
)

// Input types for MCP tools
type ListTasksInput struct {
	StatusFilter string `json:"statusFilter,omitempty"`
	TypeFilter   string `json:"typeFilter,omitempty"`
	Limit        int    `json:"limit,omitempty"`
	Offset       int    `json:"offset,omitempty"`
}

type GetTaskInput struct {
	ID string `json:"id"`
}

type CreateTaskInput struct {
	Title       string            `json:"title"`
	Description string            `json:"description,omitempty"`
	Type        string            `json:"type,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type UpdateTaskInput struct {
	ID          string            `json:"id"`
	Description string            `json:"description,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type CloseTaskInput struct {
	ID     string `json:"id"`
	Reason string `json:"reason,omitempty"`
}

// Process-related input types
type CompleteProcessInput struct {
	TaskID      string `json:"task_id"`
	ProcessName string `json:"process_name"`
	AgentID     string `json:"agent_id"`
}

type RejectProcessInput struct {
	TaskID      string `json:"task_id"`
	ProcessName string `json:"process_name"`
	AgentID     string `json:"agent_id"`
	Reason      string `json:"reason,omitempty"`
}

// JSON Schema definitions for MCP tools
var ListTasksInputSchema = &jsonschema.Schema{
	Type: "object",
	Properties: map[string]*jsonschema.Schema{
		"statusFilter": {
			Type:        "string",
			Description: "Filter tasks by status (PENDING, IN_PROGRESS, COMPLETED, REJECTED, CLOSED)",
			Enum:        []interface{}{"PENDING", "IN_PROGRESS", "COMPLETED", "REJECTED", "CLOSED"},
		},
		"typeFilter": {
			Type:        "string",
			Description: "Filter tasks by type (e.g., feature, bugfix, refactor)",
		},
		"limit": {
			Type:        "integer",
			Description: "Maximum number of tasks to return",
			Default:     intPtr(50),
			Minimum:     float64Ptr(1),
			Maximum:     float64Ptr(1000),
		},
		"offset": {
			Type:        "integer",
			Description: "Number of tasks to skip (for pagination)",
			Default:     intPtr(0),
			Minimum:     float64Ptr(0),
		},
	},
	AdditionalProperties: boolSchema(false),
}

var GetTaskInputSchema = &jsonschema.Schema{
	Type: "object",
	Properties: map[string]*jsonschema.Schema{
		"id": {
			Type:        "string",
			Description: "Task ID to retrieve",
		},
	},
	Required:             []string{"id"},
	AdditionalProperties: boolSchema(false),
}

var CreateTaskInputSchema = &jsonschema.Schema{
	Type: "object",
	Properties: map[string]*jsonschema.Schema{
		"title": {
			Type:        "string",
			Description: "Task title",
		},
		"description": {
			Type:        "string",
			Description: "Detailed task description",
		},
		"type": {
			Type:        "string",
			Description: "Task type (e.g., feature, bugfix, refactor, documentation)",
		},
		"metadata": {
			Type:                 "object",
			Description:          "Additional metadata as key-value pairs",
			AdditionalProperties: boolSchema(true),
		},
	},
	Required:             []string{"title"},
	AdditionalProperties: boolSchema(false),
}

var UpdateTaskInputSchema = &jsonschema.Schema{
	Type: "object",
	Properties: map[string]*jsonschema.Schema{
		"id": {
			Type:        "string",
			Description: "Task ID to update",
		},
		"description": {
			Type:        "string",
			Description: "Updated task description",
		},
		"metadata": {
			Type:                 "object",
			Description:          "Updated metadata as key-value pairs",
			AdditionalProperties: boolSchema(true),
		},
	},
	Required:             []string{"id"},
	AdditionalProperties: boolSchema(false),
}

var CloseTaskInputSchema = &jsonschema.Schema{
	Type: "object",
	Properties: map[string]*jsonschema.Schema{
		"id": {
			Type:        "string",
			Description: "Task ID to close",
		},
		"reason": {
			Type:        "string",
			Description: "Reason for closing the task",
		},
	},
	Required:             []string{"id"},
	AdditionalProperties: boolSchema(false),
}

var CompleteProcessInputSchema = &jsonschema.Schema{
	Type: "object",
	Properties: map[string]*jsonschema.Schema{
		"task_id": {
			Type:        "string",
			Description: "Task ID",
		},
		"process_name": {
			Type:        "string",
			Description: "Name of the process to complete (e.g., implement, review, qa)",
			Enum:        []interface{}{"implement", "review", "qa"},
		},
		"agent_id": {
			Type:        "string",
			Description: "Agent ID that is completing the process",
		},
	},
	Required:             []string{"task_id", "process_name", "agent_id"},
	AdditionalProperties: boolSchema(false),
}

var RejectProcessInputSchema = &jsonschema.Schema{
	Type: "object",
	Properties: map[string]*jsonschema.Schema{
		"task_id": {
			Type:        "string",
			Description: "Task ID",
		},
		"process_name": {
			Type:        "string",
			Description: "Name of the process to reject (e.g., implement, review, qa)",
			Enum:        []interface{}{"implement", "review", "qa"},
		},
		"agent_id": {
			Type:        "string",
			Description: "Agent ID that is rejecting the process",
		},
		"reason": {
			Type:        "string",
			Description: "Reason for rejecting the process (e.g., code quality issues, missing tests)",
		},
	},
	Required:             []string{"task_id", "process_name", "agent_id"},
	AdditionalProperties: boolSchema(false),
}

func float64Ptr(f float64) *float64 {
	return &f
}

func intPtr(i int) json.RawMessage {
	b, _ := json.Marshal(i)
	return b
}

func boolSchema(b bool) *jsonschema.Schema {
	if b {
		return &jsonschema.Schema{}
	}
	return &jsonschema.Schema{Not: &jsonschema.Schema{}}
}
