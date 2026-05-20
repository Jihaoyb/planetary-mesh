package protocol

import (
	"strings"
	"testing"
)

func TestNormalizeNodeCapabilities(t *testing.T) {
	got, err := NormalizeNodeCapabilities([]string{" role:worker ", "profile:local", "role:worker"})
	if err != nil {
		t.Fatalf("normalize capabilities: %v", err)
	}
	if strings.Join(got, ",") != "profile:local,role:worker" {
		t.Fatalf("unexpected capabilities: %+v", got)
	}

	empty, err := NormalizeNodeCapabilities(nil)
	if err != nil {
		t.Fatalf("normalize empty capabilities: %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("expected empty non-nil capabilities, got %+v", empty)
	}
}

func TestNormalizeNodeCapabilitiesRejectsInvalidLabels(t *testing.T) {
	cases := [][]string{
		{""},
		{"-bad"},
		{"bad label"},
		{strings.Repeat("a", MaxNodeCapabilityLength+1)},
	}
	for _, tc := range cases {
		if _, err := NormalizeNodeCapabilities(tc); err == nil {
			t.Fatalf("expected capabilities %q to be rejected", tc)
		}
	}
}

func TestValidateNodeLoadRejectsNegativeActiveExecutions(t *testing.T) {
	if err := ValidateNodeLoad(NodeLoad{ActiveExecutions: 0}); err != nil {
		t.Fatalf("expected zero active executions to be valid: %v", err)
	}
	if err := ValidateNodeLoad(NodeLoad{ActiveExecutions: -1}); err == nil {
		t.Fatalf("expected negative active executions to be rejected")
	}
}
