// Package audit records operational milestones without storing logs or credentials.
package audit

import (
	"bufio"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

type Event struct {
	Time            time.Time `json:"time"`
	Kind            string    `json:"kind"`
	IncidentID      string    `json:"incidentId,omitempty"`
	InvestigationID string    `json:"investigationId,omitempty"`
	PlanID          string    `json:"planId,omitempty"`
	Target          string    `json:"target,omitempty"`
	Status          string    `json:"status,omitempty"`
	Detail          string    `json:"detail,omitempty"`
}

type Journal struct {
	mu     sync.Mutex
	file   *os.File
	events []Event
	failed error
}

func Open(path string) (*Journal, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	j := &Journal{file: f}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			f.Close()
			return nil, fmt.Errorf("invalid audit journal: %w", err)
		}
		j.events = append(j.events, event)
	}
	if err := scanner.Err(); err != nil {
		f.Close()
		return nil, err
	}
	// A partial final record must not be mistaken for a committed event.
	if info, err := f.Stat(); err != nil {
		f.Close()
		return nil, err
	} else if info.Size() > 0 {
		var last [1]byte
		if _, err := f.ReadAt(last[:], info.Size()-1); err != nil || last[0] != '\n' {
			f.Close()
			return nil, errors.New("audit journal has an incomplete final record")
		}
	}
	return j, nil
}
func (j *Journal) Close() error { return j.file.Close() }
func (j *Journal) Append(e Event) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.failed != nil {
		return j.failed
	}
	e.Time = time.Now().UTC()
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if _, err = j.file.Write(data); err == nil {
		err = j.file.Sync()
	}
	if err != nil {
		j.failed = fmt.Errorf("audit journal write failed: %w", err)
		return j.failed
	}
	j.events = append(j.events, e)
	return nil
}
func (j *Journal) Routes(mux *http.ServeMux) { mux.HandleFunc("GET /api/audit", j.list) }
func (j *Journal) list(w http.ResponseWriter, r *http.Request) {
	j.mu.Lock()
	defer j.mu.Unlock()
	events := make([]Event, 0, len(j.events))
	for _, v := range slices.Backward(j.events) {
		events = append(events, v)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.MarshalWrite(w, events)
}
