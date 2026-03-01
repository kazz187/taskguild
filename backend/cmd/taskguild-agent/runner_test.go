package main

import (
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
