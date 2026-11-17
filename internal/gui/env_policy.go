package gui

import "fmt"

func (a *App) requireEnvWrite() error {
	if a.noEnvWrite {
		return fmt.Errorf("host environment writes are disabled by --no-path-fix")
	}
	return nil
}
