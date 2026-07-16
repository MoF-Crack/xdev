package linkaudit_test

import (
	"strings"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/linkaudit"
)

var gateCheckedAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func gateResult(role, status, tool string) linkaudit.Result {
	return linkaudit.Result{
		Tool:          tool,
		Version:       "1",
		OS:            "windows",
		Arch:          "amd64",
		Role:          role,
		SourceURL:     "https://example.invalid/" + tool + ".zip",
		FailureReason: status,
		CheckedAt:     gateCheckedAt,
	}
}

func TestGateDecisionForRoleStatus(t *testing.T) {
	tests := []struct {
		name   string
		role   string
		status string
		want   linkaudit.GateDecision
	}{
		{"official pass", "official", linkaudit.StatusPass, linkaudit.GatePass},
		{"mirror pass", "mirror", linkaudit.StatusPass, linkaudit.GatePass},
		{"official url dead", "official", linkaudit.StatusURLDead, linkaudit.GateBlock},
		{"official transient", "official", linkaudit.StatusTransientError, linkaudit.GateBlock},
		{"official html", "official", linkaudit.StatusHTMLInsteadOfArchive, linkaudit.GateBlock},
		{"official unsupported", "official", linkaudit.StatusUnsupportedLayout, linkaudit.GateBlock},
		{"official auth", "official", linkaudit.StatusAuthOrLicenseRequired, linkaudit.GateBlock},
		{"official hash mismatch", "official", linkaudit.StatusHashMismatch, linkaudit.GateBlock},
		{"official missing checksum warns", "official", linkaudit.StatusMissingChecksum, linkaudit.GateWarn},
		{"official impl missing warns", "official", linkaudit.StatusImplementationMissing, linkaudit.GateWarn},
		{"mirror mirror dead warns", "mirror", linkaudit.StatusMirrorDead, linkaudit.GateWarn},
		{"mirror transient warns", "mirror", linkaudit.StatusTransientError, linkaudit.GateWarn},
		{"mirror html warns", "mirror", linkaudit.StatusHTMLInsteadOfArchive, linkaudit.GateWarn},
		{"mirror unsupported warns", "mirror", linkaudit.StatusUnsupportedLayout, linkaudit.GateWarn},
		{"mirror auth warns", "mirror", linkaudit.StatusAuthOrLicenseRequired, linkaudit.GateWarn},
		{"mirror hash mismatch warns", "mirror", linkaudit.StatusHashMismatch, linkaudit.GateWarn},
		{"mirror impl missing warns", "mirror", linkaudit.StatusImplementationMissing, linkaudit.GateWarn},
		{"mirror missing checksum warns", "mirror", linkaudit.StatusMissingChecksum, linkaudit.GateWarn},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := linkaudit.GateDecisionFor(gateResult(tt.role, tt.status, "t"))
			if got != tt.want {
				t.Fatalf("GateDecisionFor(%s/%s) = %v, want %v", tt.role, tt.status, got, tt.want)
			}
		})
	}
}

func TestCIGateAggregation(t *testing.T) {
	t.Run("empty passes", func(t *testing.T) {
		o := linkaudit.CIGate(nil)
		if o.ExitCode != 0 || o.Total != 0 || o.Passed != 0 || o.Failed != 0 || o.Transient != 0 || o.Warnings != 0 {
			t.Fatalf("outcome = %+v", o)
		}
	})

	t.Run("all pass", func(t *testing.T) {
		o := linkaudit.CIGate([]linkaudit.Result{
			gateResult("official", linkaudit.StatusPass, "a"),
			gateResult("mirror", linkaudit.StatusPass, "b"),
		})
		if o.ExitCode != 0 || o.Passed != 2 || len(o.Blocking) != 0 || len(o.WarningRows) != 0 {
			t.Fatalf("outcome = %+v", o)
		}
	})

	t.Run("mirror only failures do not block", func(t *testing.T) {
		o := linkaudit.CIGate([]linkaudit.Result{
			gateResult("mirror", linkaudit.StatusMirrorDead, "m1"),
			gateResult("mirror", linkaudit.StatusImplementationMissing, "m2"),
			gateResult("mirror", linkaudit.StatusMissingChecksum, "m3"),
			gateResult("mirror", linkaudit.StatusTransientError, "m4"),
		})
		if o.ExitCode != 0 || o.Warnings != 4 || len(o.Blocking) != 0 || len(o.WarningRows) != 4 {
			t.Fatalf("outcome = %+v", o)
		}
	})

	t.Run("official url dead blocks", func(t *testing.T) {
		o := linkaudit.CIGate([]linkaudit.Result{
			gateResult("official", linkaudit.StatusPass, "ok"),
			gateResult("official", linkaudit.StatusURLDead, "dead"),
		})
		if o.ExitCode != 1 || o.Passed != 1 || o.Failed != 1 || o.Transient != 0 || len(o.Blocking) != 1 {
			t.Fatalf("outcome = %+v", o)
		}
		if o.Blocking[0].Tool != "dead" {
			t.Fatalf("blocking = %+v", o.Blocking)
		}
	})

	t.Run("official transient blocks and counts as transient", func(t *testing.T) {
		o := linkaudit.CIGate([]linkaudit.Result{
			gateResult("official", linkaudit.StatusTransientError, "t"),
		})
		if o.ExitCode != 1 || o.Failed != 0 || o.Transient != 1 || len(o.Blocking) != 1 {
			t.Fatalf("outcome = %+v", o)
		}
	})

	t.Run("mixed official and mirror", func(t *testing.T) {
		o := linkaudit.CIGate([]linkaudit.Result{
			gateResult("official", linkaudit.StatusPass, "ok"),
			gateResult("mirror", linkaudit.StatusMirrorDead, "mdead"),
			gateResult("mirror", linkaudit.StatusImplementationMissing, "mimpl"),
			gateResult("official", linkaudit.StatusHTMLInsteadOfArchive, "badhtml"),
		})
		if o.ExitCode != 1 || o.Passed != 1 || o.Failed != 1 || o.Transient != 0 || o.Warnings != 2 {
			t.Fatalf("outcome = %+v", o)
		}
		if len(o.Blocking) != 1 || o.Blocking[0].Tool != "badhtml" {
			t.Fatalf("blocking = %+v", o.Blocking)
		}
		if len(o.WarningRows) != 2 {
			t.Fatalf("warnings = %+v", o.WarningRows)
		}
	})

	t.Run("missing checksum warns even for official", func(t *testing.T) {
		o := linkaudit.CIGate([]linkaudit.Result{
			gateResult("official", linkaudit.StatusMissingChecksum, "c"),
			gateResult("official", linkaudit.StatusImplementationMissing, "d"),
		})
		if o.ExitCode != 0 || o.Warnings != 2 || len(o.Blocking) != 0 {
			t.Fatalf("outcome = %+v", o)
		}
	})

	t.Run("summary string contains counts and exit code", func(t *testing.T) {
		o := linkaudit.CIGate([]linkaudit.Result{
			gateResult("official", linkaudit.StatusPass, "a"),
			gateResult("official", linkaudit.StatusURLDead, "b"),
			gateResult("official", linkaudit.StatusTransientError, "c"),
			gateResult("mirror", linkaudit.StatusMirrorDead, "d"),
		})
		s := o.Summary()
		for _, want := range []string{"total=4", "pass=1", "fail=1", "transient=1", "warn=1", "exit=1"} {
			if !strings.Contains(s, want) {
				t.Fatalf("summary %q missing %q", s, want)
			}
		}
	})
}
