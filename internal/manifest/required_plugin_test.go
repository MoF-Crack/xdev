package manifest

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestParseRequiredPlugins_Present(t *testing.T) {
	y := `
name: flutter
display_name: Flutter
category: mobile
required_plugins:
  - java
  - android-sdk-tools
versions:
  - version: "3.0.0"
    stable: true
    archives:
      - os: windows
        arch: amd64
        format: zip
        url: https://example.com/flutter.zip
`
	var tool Tool
	if err := yaml.Unmarshal([]byte(y), &tool); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(tool.RequiredPlugin) != 2 {
		t.Fatalf("RequiredPlugin = %v, want 2 entries", tool.RequiredPlugin)
	}
	if tool.RequiredPlugin[0] != "java" || tool.RequiredPlugin[1] != "android-sdk-tools" {
		t.Errorf("RequiredPlugin = %v, want [java android-sdk-tools]", tool.RequiredPlugin)
	}
}

func TestParseRequiredPlugins_Absent(t *testing.T) {
	y := `
name: go
display_name: Go
category: language
versions:
  - version: "1.22.0"
    stable: true
    archives:
      - os: windows
        arch: amd64
        format: zip
        url: https://example.com/go.zip
`
	var tool Tool
	if err := yaml.Unmarshal([]byte(y), &tool); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if tool.RequiredPlugin != nil {
		t.Errorf("RequiredPlugin = %v, want nil", tool.RequiredPlugin)
	}
}
