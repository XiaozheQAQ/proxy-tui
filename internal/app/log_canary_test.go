package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/proxytui/proxy-tui/internal/subscription"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
)

func TestImportFailureDoesNotLeakCanaryToLogs(t *testing.T) {
	application := testApp(t)
	const canary = "PROXYTUI_TEST_SECRET_SUB_91X"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	outcomes, err := application.Import(context.Background(), subscription.ImportRequest{
		Core:  model.CoreMihomo,
		Input: server.URL + "/subscribe/" + canary + "?token=" + canary,
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(outcomes) != 1 || outcomes[0].Err == nil {
		t.Fatalf("import outcome = %+v", outcomes)
	}
	for _, entry := range application.Logs.Tail(1000) {
		if strings.Contains(entry.Format(), canary) {
			t.Fatalf("canary leaked into the application log: %s", entry.Format())
		}
	}
	if strings.Contains(outcomes[0].Err.Error(), canary) || strings.Contains(outcomes[0].Source, canary) {
		t.Fatalf("canary leaked into the import outcome: %+v", outcomes[0])
	}
}
