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

func TestNormalizeRequiredCapabilities(t *testing.T) {
	got, err := NormalizeRequiredCapabilities([]string{
		" role:text-worker ",
		"profile:local",
		"role:text-worker",
	})
	if err != nil {
		t.Fatalf("normalize required capabilities: %v", err)
	}
	if strings.Join(got, ",") != "profile:local,role:text-worker" {
		t.Fatalf("unexpected required capabilities: %+v", got)
	}

	empty, err := NormalizeRequiredCapabilities(nil)
	if err != nil {
		t.Fatalf("normalize empty required capabilities: %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("expected empty non-nil required capabilities, got %+v", empty)
	}
}

func TestNormalizeRequiredCapabilitiesRejectsInvalidLabels(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{name: "empty", in: []string{""}, want: "required capability label cannot be empty"},
		{name: "malformed", in: []string{"-bad"}, want: `invalid required capability "-bad"`},
		{
			name: "too long",
			in:   []string{strings.Repeat("a", MaxNodeCapabilityLength+1)},
			want: "exceeds 64 characters",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NormalizeRequiredCapabilities(tc.in)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}

	tooMany := make([]string, MaxNodeCapabilities+1)
	for i := range tooMany {
		tooMany[i] = "capability:" + strings.Repeat("a", i+1)
	}
	_, err := NormalizeRequiredCapabilities(tooMany)
	if err == nil || !strings.Contains(err.Error(), "required capabilities exceed maximum of 32") {
		t.Fatalf("expected maximum error, got %v", err)
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
