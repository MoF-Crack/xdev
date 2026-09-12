package cli

import "testing"

// resetGlobalCfgForTest zeroes the global run context so the next Cfg()
// call (or an explicit initContext) re-resolves the configuration.
//
// Ticket 04 (audit-fixes / P0-06) test infrastructure: the cli package
// caches the loaded config in globalCtx for the whole process, which made
// consecutive tests "stick" to the first home they touched. Tests that need
// an isolated home reset the context, then re-initialize it via initContext
// (the same path `--xmdevenv-dir <home>` takes).
//
// Defined in a _test.go file so it is only ever compiled into test builds.
func resetGlobalCfgForTest() {
	globalCtx = runCtx{}
}

// injectHome simulates `xdev --xmdevenv-dir <home>`: reset the global run
// context, then initialize it against the given home directory. The
// cleanup resets the context again so later tests never inherit this home
// (cross-case stickiness guard, ticket 04 P3).
func injectHome(t *testing.T, home string) {
	t.Helper()
	resetGlobalCfgForTest()
	t.Cleanup(resetGlobalCfgForTest)
	initContext(home, false, false, false)
}
