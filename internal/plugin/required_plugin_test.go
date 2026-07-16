package plugin

import (
	"testing"

	"github.com/xmdemt/xdev/internal/manifest"
)

func TestGenericRequiredPlugin_HasDeps(t *testing.T) {
	m := &manifest.Tool{
		Name:           "flutter",
		RequiredPlugin: []string{"java", "android-sdk-tools"},
	}
	g := NewGeneric(m)
	got := g.RequiredPlugin()
	if len(got) != 2 || got[0] != "java" || got[1] != "android-sdk-tools" {
		t.Errorf("RequiredPlugin() = %v, want [java android-sdk-tools]", got)
	}
}

func TestGenericRequiredPlugin_Empty(t *testing.T) {
	m := &manifest.Tool{
		Name:           "go",
		RequiredPlugin: nil,
	}
	g := NewGeneric(m)
	got := g.RequiredPlugin()
	if got != nil {
		t.Errorf("RequiredPlugin() = %v, want nil", got)
	}
}
