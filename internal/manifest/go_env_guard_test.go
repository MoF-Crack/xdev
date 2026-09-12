package manifest

import (
	"testing"
)

// Ticket 01 (audit-fixes / X-07): go.yaml v2 must NOT declare GOROOT.
//
// The Go archives keep their top-level go/ directory (strip: 0), so the real
// toolchain root is {{install}}\go — one level below the value the v1
// manifest used to write to the registry. A GOROOT without src/ breaks every
// `go build` with "package fmt is not in std". Go >= 1.26 self-derives GOROOT
// from the go binary location, so the correct value is "no value at all".
// GOPATH ({{install}}\..\gopath, the xdev-managed workspace outside the
// install root) is unaffected by the layout and stays.
func TestGoYAML_NoGorootEnv(t *testing.T) {
	dir := repoManifestDirT(t)
	cat, err := LoadCatalog(dir)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	goTool, ok := cat.Tool("go")
	if !ok {
		t.Fatal("go manifest not found in catalog")
	}
	if _, ok := goTool.EnvVars["GOROOT"]; ok {
		t.Errorf(`go.yaml must not declare GOROOT (got %q) — Go self-derives it and the strip:0 layout makes {{install}} one level above the real root`, goTool.EnvVars["GOROOT"])
	}
	if _, ok := goTool.EnvVars["GOPATH"]; !ok {
		t.Error("go.yaml must still declare GOPATH ({{install}}\\..\\gopath)")
	}
}
