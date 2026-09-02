package orchestrator

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// TestConnectInteractive_PasswordNeverOnArgv 守卫：密码交互式连接必须
// 通过 SSHPASS 环境变量（sshpass -e）传递密码，绝不出现在 argv 中
// （进程列表泄漏）。当 sshpass 不存在时必须返回明确错误而不是退化为 -p。
func TestConnectInteractive_PasswordNeverOnArgv(t *testing.T) {
	if _, err := exec.LookPath("sshpass"); err == nil {
		t.Skip("sshpass installed on this host — argv cannot be inspected portably")
	}
	s := &SSHAdapter{target: SSHTarget{
		Host:     "example.com",
		Port:     22,
		User:     "root",
		Password: "super-secret-123",
	}}
	err := s.ConnectInteractive(context.Background(), "ssh-example", []string{"/bin/bash"})
	if err == nil {
		t.Fatal("expected sshpass-missing error")
	}
	if !strings.Contains(err.Error(), "sshpass") {
		t.Fatalf("error should mention sshpass requirement, got: %v", err)
	}
}
