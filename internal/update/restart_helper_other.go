//go:build !windows

package update

import (
	"os"
	"os/exec"
	"time"
)

type restartParent struct{}

func restartCurrentProcessCreated() (int64, error) { return 0, errRestartUnsupported }
func openRestartParent(restartPlan) (restartParent, error) {
	return restartParent{}, errRestartUnsupported
}
func (restartParent) close()                          {}
func (restartParent) wait(time.Duration) error        { return errRestartUnsupported }
func configureRestartProcess(*exec.Cmd)               {}
func openRestartInput(string) (*os.File, error)       { return nil, errRestartUnsupported }
func openRestartMovable(string) (*os.File, error)     { return nil, errRestartUnsupported }
func renameRestartInput(*os.File, string, bool) error { return errRestartUnsupported }
func lockRestartTarget(string) (func(), error)        { return nil, errRestartUnsupported }
