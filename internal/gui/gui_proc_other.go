//go:build !windows

package gui

// killStaleWebView2 is a no-op outside Windows (WebView2 is Windows-only).
func killStaleWebView2() {}

// prewarmWebView2 is a no-op outside Windows.
func prewarmWebView2() {}
