//go:build !windows

package util

import (
	"fmt"
	"time"
)

// acquireWindowsMutex is a stub on non-Windows platforms. AcquireToolLock
// never calls it outside Windows (runtime.GOOS check), but the symbol must
// exist for the package to compile.
func acquireWindowsMutex(toolName string, timeout time.Duration) (*ToolLock, error) {
	return nil, fmt.Errorf("windows named mutex is not available on this platform (tool %q)", toolName)
}
