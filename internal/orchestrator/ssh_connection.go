package orchestrator

import (
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"sync"
)

const sshConnectionVersion = 1

// SSHConnection is the persisted, non-sensitive part of a connection. Auth
// explicitly selects a key, the client's agent/config, or an in-memory session.
// It deliberately has no password or private-key-content field.
type SSHConnection struct {
	Version    int    `json:"version"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	User       string `json:"user"`
	KeyPath    string `json:"keyPath,omitempty"`
	KnownHosts string `json:"knownHosts,omitempty"`
	Auth       string `json:"auth"` // key | agent | session
}

func connectionForTarget(t SSHTarget) *SSHConnection {
	auth := "agent"
	if t.KeyPath != "" {
		auth = "key"
	}
	if t.Password != "" {
		auth = "session"
	}
	return &SSHConnection{Version: sshConnectionVersion, Host: t.Host, Port: t.Port, User: t.User, KeyPath: t.KeyPath, KnownHosts: t.KnownHosts, Auth: auth}
}

// SSHConnectionForInstance restores targets from explicit fields only. The old
// user/host/port ID is ambiguous and is never parsed. Legacy address migration is
// read-only; callers must reconfigure records which have no authentication data.
func SSHConnectionForInstance(inst *Instance) (*SSHConnection, error) {
	if inst == nil {
		return nil, fmt.Errorf("SSH instance is required")
	}
	var c SSHConnection
	if inst.SSH != nil {
		c = *inst.SSH
		if c.Version != sshConnectionVersion {
			return nil, fmt.Errorf("unsupported SSH connection version %d; reconfigure the instance", c.Version)
		}
	} else {
		if len(inst.Addresses) != 1 || inst.Metadata["user"] == "" {
			return nil, fmt.Errorf("legacy SSH instance needs an address and user; reconfigure the connection")
		}
		host, port, err := net.SplitHostPort(inst.Addresses[0])
		if err != nil {
			return nil, fmt.Errorf("invalid legacy SSH address; reconfigure the connection: %w", err)
		}
		p, err := strconv.Atoi(port)
		if err != nil {
			return nil, fmt.Errorf("invalid legacy SSH port; reconfigure the connection")
		}
		c = SSHConnection{Version: sshConnectionVersion, Host: host, Port: p, User: inst.Metadata["user"], KeyPath: inst.Metadata["keyPath"], KnownHosts: inst.Metadata["knownHosts"]}
		if c.KeyPath != "" {
			c.Auth = "key"
		}
	}
	if c.Host == "" || c.User == "" || c.Port < 1 || c.Port > 65535 {
		return nil, fmt.Errorf("incomplete SSH connection host/port/user; reconfigure the instance")
	}
	if err := validateSSHTarget(SSHTarget{Host: c.Host, Port: c.Port, User: c.User, KeyPath: c.KeyPath, KnownHosts: c.KnownHosts}); err != nil {
		return nil, err
	}
	// Saved records have no trustworthy creation working directory. Never
	// resolve old relative paths against whichever directory the app has now.
	if (c.KeyPath != "" && !filepath.IsAbs(c.KeyPath)) || (c.KnownHosts != "" && !filepath.IsAbs(c.KnownHosts)) {
		return nil, fmt.Errorf("saved SSH file paths are relative; reconfigure the connection with explicit file paths")
	}
	switch c.Auth {
	case "key":
		if c.KeyPath == "" {
			return nil, fmt.Errorf("SSH key authentication has no key path; reconfigure the instance")
		}
	case "agent":
		if c.KeyPath != "" {
			return nil, fmt.Errorf("SSH agent authentication conflicts with a key path; reconfigure the instance")
		}
	case "session":
	default:
		return nil, fmt.Errorf("SSH authentication is missing; reconfigure the instance with a key, agent, or session password")
	}
	return &c, nil
}

// NewAdapterForInstance is the shared restoration entry for Exec, Start and
// ConnectInteractive. A session password is accepted only for an explicitly
// session-authenticated record and is never copied into the instance.
func NewAdapterForInstance(inst *Instance, sessionPassword string) (Adapter, error) {
	if inst == nil {
		return nil, fmt.Errorf("instance is required")
	}
	t := AdapterType(inst.Metadata["adapter"])
	if t == AdapterDocker {
		return NewAdapter(t)
	}
	if t != AdapterSSH && t != "" {
		return nil, fmt.Errorf("unsupported instance adapter %q", t)
	}
	c, err := SSHConnectionForInstance(inst)
	if err != nil {
		return nil, err
	}
	target := SSHTarget{Host: c.Host, Port: c.Port, User: c.User, KeyPath: c.KeyPath, KnownHosts: c.KnownHosts}
	if c.Auth == "session" {
		if sessionPassword == "" {
			return nil, fmt.Errorf("SSH session authentication expired; reconfigure the connection with a password or persistent key")
		}
		target.Password = sessionPassword
	}
	return NewSSHAdapter(target)
}

type sshSessionEntry struct {
	connection SSHConnection
	password   string
}

// SSHSession is scoped to one application lifetime. Its zero value is ready for
// use; the home, instance ID and full connection must all match before reuse.
type SSHSession struct {
	mu      sync.Mutex
	entries map[string]sshSessionEntry
}

func sshSessionKey(home, id string) string { return filepath.Clean(home) + "\x00" + id }

func (s *SSHSession) Remember(home string, inst *Instance, password string) {
	if s == nil || inst == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := sshSessionKey(home, inst.ID)
	delete(s.entries, key)
	if password == "" || inst.SSH == nil || inst.SSH.Auth != "session" {
		return
	}
	if s.entries == nil {
		s.entries = make(map[string]sshSessionEntry)
	}
	s.entries[key] = sshSessionEntry{connection: *inst.SSH, password: password}
}

func (s *SSHSession) Password(home string, inst *Instance) string {
	if s == nil || inst == nil || inst.SSH == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[sshSessionKey(home, inst.ID)]
	if !ok || entry.connection != *inst.SSH {
		return ""
	}
	return entry.password
}
