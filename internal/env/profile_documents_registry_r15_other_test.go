//go:build !windows

package env

// registeredDocumentsForTest 在非 Windows 平台没有对应的已知文件夹登记。
func registeredDocumentsForTest() string { return "" }
