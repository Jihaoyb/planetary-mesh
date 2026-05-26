package agent

import "testing"

func TestParseAllowlistAcceptsBuiltinTargets(t *testing.T) {
	got, err := ParseAllowlist("echo=builtin:echo,false=builtin:false,sleep=builtin:sleep,line-count=builtin:line-count")
	if err != nil {
		t.Fatalf("ParseAllowlist returned error: %v", err)
	}

	if got["echo"] != "builtin:echo" || got["false"] != "builtin:false" || got["sleep"] != "builtin:sleep" || got["line-count"] != "builtin:line-count" {
		t.Fatalf("unexpected allowlist: %+v", got)
	}
}

func TestParseAllowlistRejectsUnknownBuiltinTarget(t *testing.T) {
	if _, err := ParseAllowlist("echo=builtin:not-real"); err == nil {
		t.Fatalf("expected unknown built-in target to be rejected")
	}
}

func TestParseAllowlistAllowsExplicitBuiltinNamedLogicalKey(t *testing.T) {
	got, err := ParseAllowlist("builtin:echo=builtin:echo")
	if err != nil {
		t.Fatalf("ParseAllowlist returned error: %v", err)
	}
	if got["builtin:echo"] != "builtin:echo" {
		t.Fatalf("unexpected allowlist: %+v", got)
	}
}
