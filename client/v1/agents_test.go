package v1

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListAgents_DecodesInterruptResume(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"name":"codex","supports_interrupt_resume":true},{"name":"claudecode","supports_interrupt_resume":false}]`))
	}))
	defer ts.Close()

	client := New(ts.URL)
	agents, err := client.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("agents = %d, want 2", len(agents))
	}
	byName := map[string]Agent{}
	for _, a := range agents {
		byName[a.Name] = a
	}
	if !byName["codex"].SupportsInterruptResume {
		t.Fatalf("codex SupportsInterruptResume = false, want true")
	}
	if byName["claudecode"].SupportsInterruptResume {
		t.Fatalf("claudecode SupportsInterruptResume = true, want false")
	}
}
