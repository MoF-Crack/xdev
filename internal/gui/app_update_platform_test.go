package gui

import (
	"context"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/update"
)

func TestGUIUpdateNeverExposesOtherPlatformLinks(t *testing.T) {
	fixture := guiUpdateFixture()
	asset, err := update.SelectedAsset(fixture)
	if err != nil {
		t.Fatal(err)
	}
	view := guiUpdateResult(fixture)
	if view.Platform != update.TargetPlatform() || view.AssetName != asset.Name || len(view.DownloadURLs) != 1 || view.DownloadURLs[asset.Name] != asset.BrowserDownloadURL || view.Error != "" {
		t.Fatalf("multi-platform payload leaked: %+v", view)
	}
	fixture.HasUpdate = false
	view = guiUpdateResult(fixture)
	if view.AssetName != "" || len(view.DownloadURLs) != 0 {
		t.Fatalf("no-update response offered a stale archive: %+v", view)
	}
}

func TestGUIUpdateMissingPlatformFailsBeforeTargetOrApply(t *testing.T) {
	fixture := guiUpdateFixture()
	selected, err := update.SelectedAsset(fixture)
	if err != nil {
		t.Fatal(err)
	}
	var other []update.Asset
	for _, a := range fixture.Release.Assets {
		if a.Name != selected.Name {
			other = append(other, a)
		}
	}
	fixture.Release.Assets = other
	app := &App{}
	view := app.applyUpdateWith(func(context.Context, string) (*update.CheckResult, error) { return fixture, nil }, func() string { t.Error("invalid platform resolved an install target"); return "unused" }, func(context.Context, *update.CheckResult, string, func(update.Progress)) error {
		t.Error("invalid platform entered installer")
		return nil
	})
	if view.Error == "" || view.Applied || view.AssetName != "" || len(view.DownloadURLs) != 0 || !strings.Contains(view.Error, update.TargetPlatform()) {
		t.Fatalf("invalid platform did not fail closed: %+v", view)
	}
}

func TestStartupInvalidPlatformDoesNotAdvertiseManualDownloads(t *testing.T) {
	fixture := guiUpdateFixture()
	fixture.Release.Assets = nil
	desktop := &guiUpdateDesktop{}
	app := &App{desktop: desktop}
	app.emitSelfUpdate("ask", fixture)
	payload := desktop.snapshot()[0].data.(map[string]interface{})
	if payload["action"] != "applied-error" || payload["applied"] != false || payload["error"] == "" {
		t.Fatalf("invalid startup selection offered success: %+v", payload)
	}
	if payload["assetName"] != "" || len(payload["downloadUrls"].(map[string]string)) != 0 {
		t.Fatalf("startup exposed other platform/manual links: %+v", payload)
	}
}
