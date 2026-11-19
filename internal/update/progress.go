package update

import (
	"context"
	"io"
	"time"
)

// Progress is a synchronous, per-application snapshot for CLI/GUI callers.
// Percent describes archive transfer (0..99 until the application commits),
// not an estimated timer. Only a successful replacement emits completed/100.
// Unknown download length is represented by totalBytes=0 and percent=0.
// On failure the method returns an error; the last phase is NOT completion.
type Progress struct {
	Phase           string `json:"phase"`
	DownloadedBytes int64  `json:"downloadedBytes"`
	TotalBytes      int64  `json:"totalBytes"`
	Percent         int    `json:"percent"`
}

const progressInterval = 100 * time.Millisecond

type progressReporter struct {
	value      Progress
	callback   func(Progress)
	lastNotify time.Time
}

func (p *progressReporter) notify(force bool) {
	if p == nil || p.callback == nil {
		return
	}
	now := time.Now()
	if !force && now.Sub(p.lastNotify) < progressInterval {
		return
	}
	p.lastNotify = now
	p.callback(p.value)
}

func (p *progressReporter) phase(phase string) {
	if p == nil {
		return
	}
	p.value.Phase = phase
	if phase == "completed" {
		p.value.Percent = 100
	}
	p.notify(true)
}

func (p *progressReporter) downloaded(n int) {
	if p == nil {
		return
	}
	p.value.DownloadedBytes += int64(n)
	if p.value.TotalBytes > 0 {
		// Floating-point division avoids overflow for large Content-Lengths;
		// rounding down and the 99 cap never announce application success.
		percent := 100 * float64(p.value.DownloadedBytes) / float64(p.value.TotalBytes)
		if percent >= 99 {
			p.value.Percent = 99
		} else {
			p.value.Percent = int(percent)
		}
	}
	p.notify(false)
}

// contextReader also checks cancellation for in-memory/file readers whose Read
// methods do not know about contexts. The HTTP request itself remains bound to
// the same context, so a blocked network Read is cancelled by the transport.
type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(buf []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(buf)
}

type downloadProgressReader struct {
	contextReader
	progress *progressReporter
}

func (r downloadProgressReader) Read(buf []byte) (int, error) {
	n, err := r.contextReader.Read(buf)
	if n > 0 {
		r.progress.downloaded(n)
	}
	return n, err
}
