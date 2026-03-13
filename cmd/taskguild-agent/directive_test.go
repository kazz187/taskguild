package main

import (
	"errors"
	"strings"
	"testing"
)

func TestParseNextStatus(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple case",
			input:    "Some output\nNEXT_STATUS: Review",
			expected: "Review",
		},
		{
			name:     "with trailing newline",
			input:    "Some output\nNEXT_STATUS: Review\n",
			expected: "Review",
		},
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
		{
			name:     "no NEXT_STATUS",
			input:    "Some output\nDone.",
			expected: "",
		},
		{
			name:     "multiple NEXT_STATUS returns last",
			input:    "NEXT_STATUS: Draft\nSome text\nNEXT_STATUS: Review",
			expected: "Review",
		},
		{
			name:     "NEXT_STATUS in middle of text",
			input:    "Line 1\nNEXT_STATUS: Develop\nLine 3",
			expected: "Develop",
		},
		{
			name:     "NEXT_STATUS with extra spaces",
			input:    "Output\n  NEXT_STATUS:   Closed  ",
			expected: "Closed",
		},
		{
			name:     "NEXT_STATUS with no value",
			input:    "Output\nNEXT_STATUS:",
			expected: "",
		},
		{
			name:     "only NEXT_STATUS line",
			input:    "NEXT_STATUS: Review",
			expected: "Review",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseNextStatus(tt.input)
			if got != tt.expected {
				t.Errorf("parseNextStatus(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestStripNextStatus(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "strip single NEXT_STATUS at end",
			input:    "Summary text\nMore details\nNEXT_STATUS: Review",
			expected: "Summary text\nMore details",
		},
		{
			name:     "strip NEXT_STATUS with trailing newline",
			input:    "Summary text\nNEXT_STATUS: Review\n",
			expected: "Summary text",
		},
		{
			name:     "strip multiple NEXT_STATUS lines",
			input:    "NEXT_STATUS: Draft\nSome text\nNEXT_STATUS: Review",
			expected: "Some text",
		},
		{
			name:     "no NEXT_STATUS",
			input:    "Summary text\nMore details",
			expected: "Summary text\nMore details",
		},
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
		{
			name:     "only NEXT_STATUS line",
			input:    "NEXT_STATUS: Review",
			expected: "",
		},
		{
			name:     "NEXT_STATUS with leading whitespace",
			input:    "Summary\n  NEXT_STATUS: Closed  \nTrailing",
			expected: "Summary\nTrailing",
		},
		{
			name:     "preserves surrounding text",
			input:    "Line 1\nLine 2\nNEXT_STATUS: Review\nLine 4\nLine 5",
			expected: "Line 1\nLine 2\nLine 4\nLine 5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripNextStatus(tt.input)
			if got != tt.expected {
				t.Errorf("stripNextStatus(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestValidateAndResolveTransition(t *testing.T) {
	validMetadata := map[string]string{
		"_available_transitions": `[{"name":"Review"},{"name":"Develop"}]`,
	}

	tests := []struct {
		name         string
		nextStatusID string
		metadata     map[string]string
		wantName     string
		wantErr      error
	}{
		{
			name:         "exact name match",
			nextStatusID: "Review",
			metadata:     validMetadata,
			wantName:     "Review",
			wantErr:      nil,
		},
		{
			name:         "case-insensitive name match",
			nextStatusID: "review",
			metadata:     validMetadata,
			wantName:     "Review",
			wantErr:      nil,
		},
		{
			name:         "name match with different case",
			nextStatusID: "DEVELOP",
			metadata:     validMetadata,
			wantName:     "Develop",
			wantErr:      nil,
		},
		{
			name:         "name match mixed case",
			nextStatusID: "Review",
			metadata:     validMetadata,
			wantName:     "Review",
			wantErr:      nil,
		},
		{
			name:         "invalid status name",
			nextStatusID: "nonexistent",
			metadata:     validMetadata,
			wantName:     "",
			wantErr:      errInvalidTransition,
		},
		{
			name:         "empty transitions metadata",
			nextStatusID: "review",
			metadata:     map[string]string{},
			wantName:     "",
			wantErr:      nil, // generic error, not errInvalidTransition
		},
		{
			name:         "invalid JSON in transitions",
			nextStatusID: "review",
			metadata:     map[string]string{"_available_transitions": "invalid"},
			wantName:     "",
			wantErr:      nil, // generic error, not errInvalidTransition
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, err := validateAndResolveTransition(tt.nextStatusID, tt.metadata)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error wrapping %v, got nil", tt.wantErr)
				}
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("expected error wrapping %v, got %v", tt.wantErr, err)
				}
			} else if tt.wantName != "" {
				// Expect success.
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if gotName != tt.wantName {
					t.Errorf("got name %q, want %q", gotName, tt.wantName)
				}
			} else {
				// Expect some generic error (not errInvalidTransition).
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				if errors.Is(err, errInvalidTransition) {
					t.Errorf("expected generic error, got errInvalidTransition: %v", err)
				}
			}
		})
	}
}

func TestBuildTransitionRetryPrompt(t *testing.T) {
	t.Run("with valid transitions", func(t *testing.T) {
		metadata := map[string]string{
			"_available_transitions": `[{"name":"Review"},{"name":"Closed"}]`,
		}
		prompt := buildTransitionRetryPrompt("invalid_status", metadata)

		// Should mention the failed status.
		if !strings.Contains(prompt, "invalid_status") {
			t.Error("prompt should contain the failed status ID")
		}
		// Should list valid transition names.
		if !strings.Contains(prompt, "Review") {
			t.Error("prompt should contain valid transition name 'Review'")
		}
		if !strings.Contains(prompt, "Closed") {
			t.Error("prompt should contain valid transition name 'Closed'")
		}
		// Should request NEXT_STATUS format.
		if !strings.Contains(prompt, "NEXT_STATUS:") {
			t.Error("prompt should contain NEXT_STATUS format instruction")
		}
	})

	t.Run("with empty transitions metadata", func(t *testing.T) {
		metadata := map[string]string{}
		prompt := buildTransitionRetryPrompt("invalid_status", metadata)

		// Should still mention the failed status.
		if !strings.Contains(prompt, "invalid_status") {
			t.Error("prompt should contain the failed status ID")
		}
		// Should request NEXT_STATUS format even without transition list.
		if !strings.Contains(prompt, "NEXT_STATUS:") {
			t.Error("prompt should contain NEXT_STATUS format instruction")
		}
	})
}
