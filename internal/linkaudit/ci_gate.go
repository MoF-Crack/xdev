package linkaudit

import (
	"fmt"
	"io"
)

// GateDecision classifies a single audit Result under the CI release gate
// defined in docs/download-source-policy.md.
type GateDecision int

const (
	// GatePass means the source is healthy and the gate advances.
	GatePass GateDecision = iota
	// GateWarn means the row is reported to stderr but does not block the
	// build. Mirror failures and known implementation gaps warn.
	GateWarn
	// GateBlock means the row blocks the build (exit code 1). Any
	// non-passing official source blocks.
	GateBlock
)

func (d GateDecision) String() string {
	switch d {
	case GatePass:
		return "PASS"
	case GateWarn:
		return "WARN"
	case GateBlock:
		return "BLOCK"
	default:
		return "UNKNOWN"
	}
}

// GateDecisionFor maps a Result to a CI gate decision. Official sources must
// be healthy: every non-PASS status except MISSING_CHECKSUM and
// IMPLEMENTATION_MISSING blocks. Mirror sources are best-effort: any
// non-PASS status only warns. MISSING_CHECKSUM and IMPLEMENTATION_MISSING
// always warn regardless of role (probe mode never verifies SHA, and
// unimplemented mirror rewrites are a known configuration gap).
func GateDecisionFor(r Result) GateDecision {
	switch r.FailureReason {
	case StatusPass:
		return GatePass
	case StatusImplementationMissing, StatusMissingChecksum:
		return GateWarn
	}
	if r.Role == "mirror" {
		return GateWarn
	}
	return GateBlock
}

// GateOutcome is the aggregate result of evaluating CIGate over a report.
type GateOutcome struct {
	ExitCode    int
	Total       int
	Passed      int
	Failed      int // blocking deterministic failures on official sources
	Transient   int // blocking TRANSIENT_ERROR failures on official sources
	Warnings    int // non-blocking rows (mirror dead, impl missing, missing checksum)
	Blocking    []Result
	WarningRows []Result
}

// CIGate evaluates probe-mode audit results against the release gate.
// The gate blocks (ExitCode 1) when any official source is dead, transient,
// or otherwise deterministically broken. Mirror and known-gap statuses warn
// without blocking. The outcome's counts and row lists let callers print a
// precise failure list.
func CIGate(results []Result) GateOutcome {
	var o GateOutcome
	o.Total = len(results)
	for _, r := range results {
		switch GateDecisionFor(r) {
		case GatePass:
			o.Passed++
		case GateWarn:
			o.Warnings++
			o.WarningRows = append(o.WarningRows, r)
		case GateBlock:
			if r.FailureReason == StatusTransientError {
				o.Transient++
			} else {
				o.Failed++
			}
			o.Blocking = append(o.Blocking, r)
		}
	}
	if len(o.Blocking) > 0 {
		o.ExitCode = 1
	}
	return o
}

// Summary returns a single-line machine-greppable summary of the gate
// outcome, e.g. "audit gate: total=4 pass=1 fail=1 transient=1 warn=1 exit=1".
func (o GateOutcome) Summary() string {
	return fmt.Sprintf("audit gate: total=%d pass=%d fail=%d transient=%d warn=%d exit=%d",
		o.Total, o.Passed, o.Failed, o.Transient, o.Warnings, o.ExitCode)
}

// Print writes the summary line followed by one line per blocking and
// warning row. Blocking rows are prefixed "BLOCK"; warning rows "WARN".
// PASS rows are omitted to keep the output focused on action items.
func (o GateOutcome) Print(w io.Writer) {
	fmt.Fprintln(w, o.Summary())
	for _, r := range o.Blocking {
		fmt.Fprintf(w, "  BLOCK  %s %s %s/%s %s -> %s  (%s)\n",
			r.Tool, r.Version, r.OS, r.Arch, r.Role, r.FailureReason, r.SourceURL)
	}
	for _, r := range o.WarningRows {
		fmt.Fprintf(w, "  WARN   %s %s %s/%s %s -> %s  (%s)\n",
			r.Tool, r.Version, r.OS, r.Arch, r.Role, r.FailureReason, r.SourceURL)
	}
}
