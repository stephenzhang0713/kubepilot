package tui

import (
	"bytes"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/teatest/v2"
	"github.com/stephenzhang0713/kubepilot/internal/config"
)

func TestTerminalSettingsFlow(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/kubeconfig", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{"configured":false}`) })
	mux.HandleFunc("GET /api/cluster/overview", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{"sections":[]}`) })
	mux.HandleFunc("GET /api/inspection", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{"enabled":false}`) })
	path := filepath.Join(t.TempDir(), "config.json")
	model := New(mux, "initial", false, WithSettings(config.Defaults(), path))
	tm := teatest.NewTestModel(t, model, teatest.WithInitialTermSize(100, 28), teatest.WithProgramOptions(tea.WithColorProfile(colorprofile.ANSI256)))
	t.Cleanup(func() { _ = tm.Quit() })
	wait := func(want string) {
		t.Helper()
		teatest.WaitFor(t, tm.Output(), func(out []byte) bool { return bytes.Contains([]byte(ansi.Strip(string(out))), []byte(want)) }, teatest.WithDuration(3*time.Second))
	}
	wait("Overview updated")
	tm.Send(tea.KeyPressMsg{Code: '5', Text: "5"})
	wait("CONFIGURATION")
	// Queue a completed inspection response before the key presses, so this
	// remains deterministic even if the renderer coalesces intermediate frames.
	tm.Send(response{action: "settings", data: []byte(`{"enabled":false}`)})
	tm.Send(tea.KeyPressMsg{Code: tea.KeyDown})
	tm.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
	wait("Edit Model name")
	tm.Send(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	tm.Send(tea.PasteMsg{Content: "👩‍💻"})
	tm.Send(tea.KeyPressMsg{Code: tea.KeyBackspace})
	tm.Send(tea.PasteMsg{Content: "new-model"})
	tm.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
	wait("Saved. Restart")
	tm.Send(tea.KeyPressMsg{Code: 'q', Text: "q"})
	final := tm.FinalModel(t, teatest.WithFinalTimeout(3*time.Second)).(Model)
	if final.settings.ModelName != "new-model" {
		t.Fatalf("unexpected saved model: %q", final.settings.ModelName)
	}
	saved, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ModelName != "new-model" {
		t.Fatalf("unexpected persisted model: %q", saved.ModelName)
	}
}
