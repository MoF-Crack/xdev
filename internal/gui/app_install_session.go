package gui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/xmdemt/xdev/internal/installer"
)

// InstallSession is the authoritative snapshot. Starting/accepting an operation
// is not completion. Revision lets clients discard reordered progress snapshots.
type InstallSession struct {
	SessionID   string  `json:"sessionId"`
	TaskID      string  `json:"taskId"`
	Tool        string  `json:"tool"`
	Version     string  `json:"version"`
	ResourceKey string  `json:"resourceKey"`
	Status      string  `json:"status"`
	Phase       string  `json:"phase"`
	Percent     float64 `json:"percent"`
	Speed       string  `json:"speed"`
	ETA         string  `json:"eta"`
	Error       string  `json:"error,omitempty"`
	CanCancel   bool    `json:"canCancel"`
	CanPause    bool    `json:"canPause"`
	Paused      bool    `json:"paused"`
	Terminal    bool    `json:"terminal"`
	Revision    uint64  `json:"revision"`
}

type installSession struct {
	id     string
	view   InstallSession
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

// installSessions is owned by one App. Its zero value is ready for use. IDs are
// retained for the lifetime of the App: a completed ID cannot be reused by a
// retry, and a terminal snapshot remains queryable after the download is gone.
type installSessions struct {
	mu        sync.Mutex
	sessions  map[string]*installSession
	resources map[string]string
}

func (s *installSessions) begin(ctx context.Context, spec InstallSpec, resource string) (*installSession, error) {
	id := spec.SessionID
	if id == "" {
		var token [16]byte
		if _, err := rand.Read(token[:]); err != nil {
			return nil, err
		}
		id = hex.EncodeToString(token[:])
	}
	if len(id) > 128 || strings.TrimSpace(id) != id {
		return nil, fmt.Errorf("invalid install session ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions == nil {
		s.sessions = make(map[string]*installSession)
		s.resources = make(map[string]string)
	}
	if _, exists := s.sessions[id]; exists {
		return nil, fmt.Errorf("install session %s already exists", id)
	}
	if owner := s.resources[resource]; owner != "" {
		return nil, fmt.Errorf("installation busy for %s (session %s)", resource, owner)
	}
	ctx, cancel := context.WithCancel(ctx)
	session := &installSession{id: id, ctx: ctx, cancel: cancel, done: make(chan struct{}), view: InstallSession{
		SessionID: id, TaskID: id, Tool: spec.Tool, Version: spec.Version, ResourceKey: resource,
		Status: "accepted", Phase: "Preparing", CanCancel: true, Revision: 1,
	}}
	s.sessions[id] = session
	s.resources[resource] = id
	return session, nil
}

func (s *installSessions) snapshot(id string) (InstallSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	x := s.sessions[id]
	if x == nil {
		return InstallSession{}, fmt.Errorf("install session %s not found", id)
	}
	return x.view, nil
}

func (s *installSessions) update(id string, change func(*InstallSession)) (InstallSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	x := s.sessions[id]
	if x == nil || x.view.Terminal {
		return InstallSession{}, false
	}
	change(&x.view)
	x.view.Revision++
	return x.view, true
}

// commit and requestCancel share the same lock. Once commit wins, a cancel
// request is rejected; if cancellation wins, no install/activation may begin.
func (s *installSessions) commit(id string) (InstallSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	x := s.sessions[id]
	if x == nil {
		return InstallSession{}, fmt.Errorf("install session %s not found", id)
	}
	if x.ctx.Err() != nil {
		return x.view, x.ctx.Err()
	}
	if x.view.Terminal {
		return x.view, fmt.Errorf("install session is already terminal")
	}
	x.view.Status, x.view.Phase = "committing", "Installing"
	x.view.CanCancel, x.view.CanPause, x.view.Paused = false, false, false
	x.view.Revision++
	return x.view, nil
}

func (s *installSessions) requestCancel(id string) (InstallSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	x := s.sessions[id]
	if x == nil {
		return InstallSession{}, fmt.Errorf("install session %s not found", id)
	}
	if x.view.Status == "cancelling" {
		return x.view, nil
	}
	if x.view.Terminal || !x.view.CanCancel {
		return x.view, fmt.Errorf("installation has entered commit or finished; cancellation is unavailable")
	}
	x.cancel()
	x.view.Status, x.view.Phase = "cancelling", "Cancelling"
	x.view.CanCancel, x.view.CanPause, x.view.Paused = false, false, false
	x.view.Revision++
	return x.view, nil
}

func (s *installSessions) finish(id string, err error) (InstallSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	x := s.sessions[id]
	if x == nil || x.view.Terminal {
		return InstallSession{}, false
	}
	// An accepted cancellation wins even if a cached/no-op worker returned
	// nil concurrently. Committed work never accepts user cancellation.
	if x.view.Status == "cancelling" {
		err = context.Canceled
	}
	x.err = err
	x.view.Status, x.view.Phase = "completed", "Completed"
	x.view.Percent = 100
	if err != nil {
		x.view.Error = err.Error()
		switch {
		case errors.Is(err, installer.ErrIncompleteInstall):
			x.view.Status, x.view.Phase = "needs-repair", "Incomplete"
		case errors.Is(err, context.Canceled):
			x.view.Status, x.view.Phase = "cancelled", "Cancelled"
		default:
			x.view.Status, x.view.Phase = "error", "error"
		}
	}
	x.view.Terminal, x.view.CanCancel, x.view.CanPause, x.view.Paused = true, false, false, false
	x.view.Revision++
	delete(s.resources, x.view.ResourceKey)
	x.cancel()
	close(x.done)
	return x.view, true
}

// StartInstall accepts an operation and returns its identity. The frontend
// supplies SessionID and subscribes before calling this binding; the snapshot
// binding also covers fast completion before the start response arrives.
func (a *App) StartInstall(spec InstallSpec) (string, error) {
	spec, resource, err := a.normalizeInstallSpec(spec)
	if err != nil {
		return "", err
	}
	session, err := a.launchInstallSession(spec, resource, func(ctx context.Context, id string) error {
		switch spec.Kind {
		case "tool":
			return a.runToolInstall(ctx, id, spec)
		case "package-manager", "sub-tool":
			if err := a.commitInstallSession(id); err != nil {
				return err
			}
			var err error
			if spec.Kind == "sub-tool" {
				err = a.installSubToolContext(ctx, spec)
			} else {
				err = spec.pmDefinition.checkExecution(spec.PMDigest)
				if err == nil {
					err = a.installPackageManagerViaScriptContext(ctx, *spec.pmDefinition)
				}
			}
			return err
		default:
			return fmt.Errorf("unknown install kind %q", spec.Kind)
		}
	})
	if err != nil {
		return "", err
	}
	return session.id, nil
}

func (a *App) normalizeInstallSpec(spec InstallSpec) (InstallSpec, string, error) {
	if strings.TrimSpace(spec.Tool) == "" {
		return spec, "", fmt.Errorf("install spec: tool is required")
	}
	if spec.Kind == "" {
		spec.Kind = "tool"
	}
	resource := spec.Tool
	switch spec.Kind {
	case "package-manager":
		pm, found, err := a.lookupPM(spec.Tool)
		if err != nil {
			return spec, "", err
		}
		if found {
			if err := pm.checkExecution(spec.PMDigest); err != nil {
				return spec, "", err
			}
			spec.pmDefinition = &pm
			if pm.installCommand != "" {
				break
			}
			if pm.definition.Origin.Authorize("") != nil {
				return spec, "", fmt.Errorf("%s has no script installation", spec.Tool)
			}
			if pm.toolName != "" {
				spec.Tool = pm.toolName
			}
		}
		spec.Kind = "tool"
		resource = spec.Tool
	case "sub-tool":
		pm, found, err := a.lookupPM(spec.Tool)
		if err != nil {
			return spec, "", err
		}
		if !found {
			return spec, "", fmt.Errorf("unknown sub-tool %q", spec.Tool)
		}
		if err := pm.checkExecution(spec.PMDigest); err != nil {
			return spec, "", err
		}
		spec.pmDefinition = &pm
		// Corepack/npm modify the parent Node installation; pip modifies
		// Python. Use the actual writer's resource, not the UI alias.
		resource = pm.parentTool
		switch spec.Method {
		case "npm", "corepack":
			resource = "node"
		case "pip":
			resource = "python"
		case "uv":
			resource = "uv"
		}
		if resource == "" {
			resource = spec.Tool
		}
	case "tool":
	default:
		return spec, "", fmt.Errorf("install spec: unknown kind %q", spec.Kind)
	}
	if spec.Kind == "tool" {
		p, err := a.mustGetPlugin(spec.Tool)
		if err != nil {
			return spec, "", err
		}
		resource = p.Name()
		if spec.Version == "" {
			spec.Version = p.DefaultVersion()
		}
		if spec.Version == "" {
			spec.Version = "latest"
		}
	}
	return spec, strings.ToLower(resource), nil
}

func (a *App) launchInstallSession(spec InstallSpec, resource string, run func(context.Context, string) error) (*installSession, error) {
	x, err := a.installSessions.begin(a.rootContext(), spec, resource)
	if err != nil {
		return nil, err
	}
	id := x.view.SessionID
	a.emitInstallSession(x.view)
	go func() {
		var runErr error
		defer func() {
			if recovered := recover(); recovered != nil {
				runErr = fmt.Errorf("installation failed: %v", recovered)
			}
			// Partial/error installs may also have changed disk state. Invalidate
			// before the terminal event triggers frontend refreshes.
			a.invalidateInstallQueries()
			if view, changed := a.installSessions.finish(id, runErr); changed {
				a.emitInstallSession(view)
				// Preserve terminal notifications for older generated callers too.
				if view.Status == "completed" {
					a.emitEvent("installProgress", InstallProgress{TaskID: id, Tool: view.Tool, Version: view.Version, Phase: "Completed", Percent: 100})
				} else {
					a.emitInstallError(id, view.Tool, view.Version, view.Error)
				}
			}
		}()
		runErr = run(x.ctx, id)
	}()
	return x, nil
}

func (a *App) GetInstallSession(sessionID string) (InstallSession, error) {
	return a.installSessions.snapshot(sessionID)
}

func (a *App) CancelInstall(sessionID string) error {
	view, err := a.installSessions.requestCancel(sessionID)
	if err != nil {
		return err
	}
	a.emitInstallSession(view)
	// The worker observes its cancelled context, waits for downloader cleanup
	// and only then publishes the terminal cancelled state. This is an ACK.
	return nil
}

func (a *App) commitInstallSession(id string) error {
	view, err := a.installSessions.commit(id)
	if err == nil {
		a.emitInstallSession(view)
	}
	return err
}

func (a *App) emitInstallSession(view InstallSession) {
	a.emitEvent("installSession", view)
}

func (a *App) controlInstallDownload(id string, resume bool) error {
	a.installSessions.mu.Lock()
	x := a.installSessions.sessions[id]
	if x == nil {
		a.installSessions.mu.Unlock()
		if resume {
			return a.dlMgr.Resume(id)
		}
		return a.dlMgr.Pause(id)
	}
	if x.view.Terminal || !x.view.CanPause {
		phase := x.view.Phase
		a.installSessions.mu.Unlock()
		return fmt.Errorf("download controls unavailable in phase %s", phase)
	}
	var err error
	if resume {
		err = a.dlMgr.Resume(id)
	} else {
		err = a.dlMgr.Pause(id)
	}
	if err == nil {
		x.view.Paused = !resume
		if resume {
			x.view.Phase = "Downloading"
		} else {
			x.view.Phase = "Paused"
		}
		x.view.Revision++
	}
	view := x.view
	a.installSessions.mu.Unlock()
	if err == nil {
		a.emitInstallSession(view)
	}
	return err
}

func (a *App) invalidateInstallQueries() {
	a.invalidateDashboardCache()
	a.InvalidateSizeBreakdown()
	a.InvalidateToolsCache()
}
