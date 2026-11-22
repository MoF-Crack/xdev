//go:build !windows

package extractor

func retryPublish(err error) bool { return false }
