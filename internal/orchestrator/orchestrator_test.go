package orchestrator

import (
	"strings"
	"testing"
)

func TestTypes(t *testing.T) {
	spec := EnvironmentSpec{
		Image: "ubuntu:22.04",
		Tools: map[string]string{"gcc": "12", "python": "3.11"},
		Env:   map[string]string{"FOO": "bar"},
		Ports: []int{8080, 9090},
		Resources: ResourceSpec{
			CPUs:   2.0,
			Memory: "4g",
			Disk:   "20g",
		},
		SSHTarget: SSHTarget{
			Host:       "example.com",
			Port:       2222,
			User:       "dev",
			KeyPath:    "/home/dev/.ssh/id_ed25519",
			KnownHosts: "/home/dev/.ssh/known_hosts",
		},
		WorkingDir: "/workspace",
	}
	if spec.Image != "ubuntu:22.04" {
		t.Errorf("Image = %q, want %q", spec.Image, "ubuntu:22.04")
	}
	if spec.Tools["gcc"] != "12" {
		t.Errorf("Tools[gcc] = %q, want %q", spec.Tools["gcc"], "12")
	}
	if spec.Env["FOO"] != "bar" {
		t.Errorf("Env[FOO] = %q, want %q", spec.Env["FOO"], "bar")
	}
	if len(spec.Ports) != 2 || spec.Ports[0] != 8080 || spec.Ports[1] != 9090 {
		t.Errorf("Ports = %v, want [8080 9090]", spec.Ports)
	}
	if spec.Resources.CPUs != 2.0 {
		t.Errorf("Resources.CPUs = %v, want 2.0", spec.Resources.CPUs)
	}
	if spec.Resources.Memory != "4g" {
		t.Errorf("Resources.Memory = %q, want %q", spec.Resources.Memory, "4g")
	}
	if spec.Resources.Disk != "20g" {
		t.Errorf("Resources.Disk = %q, want %q", spec.Resources.Disk, "20g")
	}
	if spec.SSHTarget.Host != "example.com" {
		t.Errorf("SSHTarget.Host = %q, want %q", spec.SSHTarget.Host, "example.com")
	}
	if spec.SSHTarget.Port != 2222 {
		t.Errorf("SSHTarget.Port = %d, want 2222", spec.SSHTarget.Port)
	}
	if spec.WorkingDir != "/workspace" {
		t.Errorf("WorkingDir = %q, want %q", spec.WorkingDir, "/workspace")
	}

	inst := Instance{
		ID:        "test-id-123",
		Status:    "running",
		Addresses: []string{"192.168.1.100", "10.0.0.5"},
		Metadata:  map[string]string{"adapter": "docker"},
	}
	if inst.ID != "test-id-123" {
		t.Errorf("ID = %q, want %q", inst.ID, "test-id-123")
	}
	if inst.Status != "running" {
		t.Errorf("Status = %q, want %q", inst.Status, "running")
	}
	if len(inst.Addresses) != 2 {
		t.Errorf("Addresses len = %d, want 2", len(inst.Addresses))
	}
	if inst.Metadata["adapter"] != "docker" {
		t.Errorf("Metadata[adapter] = %q, want %q", inst.Metadata["adapter"], "docker")
	}

	res := ResourceSpec{CPUs: 4.5, Memory: "8g", Disk: "100g"}
	if res.CPUs != 4.5 {
		t.Errorf("CPUs = %v, want 4.5", res.CPUs)
	}

	ssh := SSHTarget{Host: "host", Port: 22, User: "root"}
	if ssh.Host != "host" {
		t.Errorf("Host = %q, want %q", ssh.Host, "host")
	}

	execRes := ExecResult{Stdout: "hello", Stderr: "", ExitCode: 0}
	if execRes.Stdout != "hello" {
		t.Errorf("Stdout = %q, want %q", execRes.Stdout, "hello")
	}
	if execRes.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", execRes.ExitCode)
	}
}

func TestDockerAdapterName(t *testing.T) {
	d := &DockerAdapter{}
	if name := d.Name(); name != "docker" {
		t.Errorf("DockerAdapter.Name() = %q, want %q", name, "docker")
	}
}

func TestSSHAdapterName(t *testing.T) {
	s := &SSHAdapter{target: SSHTarget{Host: "localhost"}}
	if name := s.Name(); name != "ssh" {
		t.Errorf("SSHAdapter.Name() = %q, want %q", name, "ssh")
	}
}

func TestDockerAdapterCreateNoDocker(t *testing.T) {
	if IsDockerAvailable() {
		t.Skip("Docker is available; skipping no-Docker error test")
	}
	_, err := NewDockerAdapter()
	if err == nil {
		t.Fatal("NewDockerAdapter() expected error when Docker is not available, got nil")
	}
}

func TestSSHAdapterExecNoHost(t *testing.T) {
	_, err := NewSSHAdapter(SSHTarget{Host: ""})
	if err == nil {
		t.Fatal("NewSSHAdapter with empty host expected error, got nil")
	}
	if !strings.Contains(err.Error(), "host") {
		t.Errorf("error %q should mention host", err.Error())
	}
}
