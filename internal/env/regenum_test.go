//go:build windows

// Ticket 05 (audit-fixes / P0-07 + N-10) guard tests: RegEnumValueW
// robustness in GetAllUserEnvVars and EnumEnvVars.
//
// The historical bugs: GetAllUserEnvVars used a 256-char name buffer and,
// on ERROR_MORE_DATA, sliced nameBuf[:nameBufSize] with the API-written
// required length > 256 → panic (slice bounds out of range) on every
// Windows shim execution. EnumEnvVars passed the wrong byte size
// (len(buf) instead of len(buf)*2) and silently broke the whole
// enumeration on values larger than the buffer.
package env

import (
	"strings"
	"testing"
)

// setLongTestVar writes a registry value through the trusted path (the
// 300-char names used here are not in the write whitelist) and registers
// cleanup.
func setLongTestVar(t *testing.T, name, value string) {
	t.Helper()
	if err := SetUserEnvVarTrusted(name, value); err != nil {
		t.Fatalf("SetUserEnvVarTrusted(%d-char name): %v", len(name), err)
	}
	t.Cleanup(func() {
		if err := DeleteUserEnvVarTrusted(name); err != nil {
			t.Errorf("cleanup DeleteUserEnvVarTrusted(%d-char name): %v", len(name), err)
		}
	})
}

// R1: a 300-character value name must not panic GetAllUserEnvVars (the
// hot path of every Windows shim execution) and must be returned whole.
func TestGetAllUserEnvVars_LongValueName_NoPanic(t *testing.T) {
	const totalLen = 300
	name := "XDEVTESTREGENUM" + strings.Repeat("A", totalLen-15) // 15-char prefix + padding
	if len(name) != totalLen {
		t.Fatalf("test name length = %d, want %d", len(name), totalLen)
	}
	setLongTestVar(t, name, "longname-value")

	all := GetAllUserEnvVars() // previously: panic: slice bounds out of range
	got, ok := all[name]
	if !ok || got != "longname-value" {
		t.Errorf("300-char value name not enumerated correctly: present=%v value=%q", ok, got)
	}
}

// R2: a name of exactly 256 chars (the old buffer size — the historical
// boundary case) must not panic either. With the retry it is returned whole.
func TestGetAllUserEnvVars_Exactly256_TruncatedNotPanic(t *testing.T) {
	name := "XDEVTESTREGENUM256" + strings.Repeat("B", 256-18)
	if len(name) != 256 {
		t.Fatalf("test name length = %d, want 256", len(name))
	}
	setLongTestVar(t, name, "boundary-value")

	all := GetAllUserEnvVars() // must not panic
	got, ok := all[name]
	if !ok || got != "boundary-value" {
		t.Errorf("256-char value name not enumerated correctly: present=%v value=%q", ok, got)
	}
}

// R3: value data larger than the 64 KiB buffer must not silently truncate
// the EnumEnvVars enumeration — the value is returned whole and the rest
// of the enumeration survives (a small marker proves the list is not
// cut short).
func TestEnumEnvVars_LargeValueData(t *testing.T) {
	bigValue := strings.Repeat("C", 70000) // 70,000 chars ≈ 140 KB UTF-16
	setLongTestVar(t, "XDEVTESTREGENUMBIG", bigValue)
	setLongTestVar(t, "XDEVTESTREGENUMSMALL", "small-marker")

	pairs, err := EnumEnvVars("user")
	if err != nil {
		t.Fatalf("EnumEnvVars: %v", err)
	}

	var big, small bool
	for _, p := range pairs {
		switch p.Name {
		case "XDEVTESTREGENUMBIG":
			big = true
			if len(p.Value) != len(bigValue) || !strings.HasPrefix(p.Value, "CCC") {
				t.Errorf("large value truncated: got %d chars, want %d", len(p.Value), len(bigValue))
			}
		case "XDEVTESTREGENUMSMALL":
			small = true
			if p.Value != "small-marker" {
				t.Errorf("small marker value = %q, want %q", p.Value, "small-marker")
			}
		}
	}
	if !big {
		t.Error("70KB value missing from EnumEnvVars result (silent truncation regression)")
	}
	if !small {
		t.Error("enumeration aborted after the large value — the list must not be cut short")
	}
}
