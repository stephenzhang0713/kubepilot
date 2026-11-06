package agent

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stephenzhang0713/kubepilot/internal/investigation"
)

func TestChatSendsEvidenceAndConversation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		for _, wanted := range []string{"backoff", "What happened?", "untrusted data"} {
			if !strings.Contains(string(body), wanted) {
				t.Errorf("missing %q in request", wanted)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"1","object":"chat.completion","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"Check events."}}]}`)
	}))
	defer server.Close()
	a := Analyzer{BaseURL: server.URL, Model: "test", APIKey: "test"}
	answer, err := a.Chat(t.Context(), investigation.Result{Evidence: investigation.Evidence{Logs: "backoff"}}, []Turn{{Role: "user", Text: "What happened?"}})
	if err != nil || answer != "Check events." {
		t.Fatalf("answer %q: %v", answer, err)
	}
}
