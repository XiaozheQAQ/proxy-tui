package pages

import (
	"strings"
	"testing"

	"github.com/proxytui/proxy-tui/internal/core"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
	"github.com/proxytui/proxy-tui/internal/tui/layout"
)

func TestOverviewRedactsSubscriptionSourceToken(t *testing.T) {
	const canary = "PROXYTUI_TEST_TOKEN_53Z"
	page := NewOverview()
	page.Update(DataMsg{Overview: OverviewData{
		Core:      model.CoreMihomo,
		Status:    core.Status{Installed: true, Running: true},
		Active:    model.Profile{ID: "p1", Core: model.CoreMihomo, Source: model.Source{Kind: model.SourceURL, URL: "https://example.com/sub/" + canary + "?token=" + canary}},
		HasActive: true,
	}})
	out := page.View(layout.ThemeByName("dark"), 120, 40)
	if strings.Contains(out, canary) {
		t.Fatalf("overview leaked the subscription token: %s", out)
	}
}
