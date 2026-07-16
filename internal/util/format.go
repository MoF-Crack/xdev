package util

import (
	"fmt"
	"time"
)

// HumanBytesStr formats a byte count in IEC binary units (KiB/MiB/GiB/TiB).
func HumanBytesStr(n int64) string {
	const (
		KiB = 1 << 10
		MiB = 1 << 20
		GiB = 1 << 30
		TiB = 1 << 40
	)
	switch {
	case n >= TiB:
		return fmt.Sprintf("%.2f TiB", float64(n)/TiB)
	case n >= GiB:
		return fmt.Sprintf("%.2f GiB", float64(n)/GiB)
	case n >= MiB:
		return fmt.Sprintf("%.2f MiB", float64(n)/MiB)
	case n >= KiB:
		return fmt.Sprintf("%.2f KiB", float64(n)/KiB)
	}
	return fmt.Sprintf("%d B", n)
}

// HumanSpeedStr returns a human-readable download speed string.
func HumanSpeedStr(downloaded int64, elapsed time.Duration) string {
	if elapsed == 0 {
		return ""
	}
	bps := float64(downloaded) / elapsed.Seconds()
	return HumanBytesStr(int64(bps)) + "/s"
}

// HumanETAStr returns a human-readable estimated time remaining string.
func HumanETAStr(downloaded, total int64, elapsed time.Duration) string {
	if downloaded == 0 || total == 0 || downloaded >= total {
		return ""
	}
	remaining := total - downloaded
	bps := float64(downloaded) / elapsed.Seconds()
	if bps == 0 {
		return ""
	}
	eta := time.Duration(float64(remaining)/bps) * time.Second
	return FormatDurationStr(eta)
}

// FormatDurationStr formats a duration in a compact human-readable form (e.g. "2h3m5s").
func FormatDurationStr(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second
	if h > 0 {
		return fmt.Sprintf("%dh%dm%ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}
