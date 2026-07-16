package gui

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestInstallProgressSerializesTaskID(t *testing.T) {
	b, err := json.Marshal(InstallProgress{TaskID: "node@22", Tool: "node", Version: "22"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"taskId":"node@22"`) {
		t.Fatalf("missing taskId: %s", b)
	}
}

func TestInstallEventPayloadsInSourceContainTaskID(t *testing.T) {
	b, err := os.ReadFile("app_install.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Count(s, "TaskID:  taskID") < 3 {
		t.Fatal("every InstallProgress emission must carry taskID")
	}
	if strings.Count(s, `"taskId":  taskID`) < 2 {
		t.Fatal("every installError emission must carry taskId")
	}
}
