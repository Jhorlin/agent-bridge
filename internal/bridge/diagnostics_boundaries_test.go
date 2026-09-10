package bridge

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Jhorlin/agent-bridge/internal/diagnostics"
)

func TestObservedSafetyStopsHaveActionableCodesWithoutWrites(t *testing.T) {
	for _, spec := range []struct {
		name, code string
		sentinel   error
	}{
		{"conflict", "conflict", ErrConflicts},
		{"pending", "pending_recovery", ErrRecoveryPending},
		{"stale", "observation_changed", ErrObservationChanged},
	} {
		t.Run(spec.name, func(t *testing.T) {
			f := newFixture(t)
			f.apply()
			options := Options{}
			switch spec.name {
			case "conflict":
				f.write("CLAUDE.md", "CANARY_LEFT")
				f.write("AGENTS.md", "CANARY_RIGHT")
			case "pending":
				f.write("state/pending.json", `{"transaction":"CANARY_PENDING"}`)
			case "stale":
				review, err := ReviewProfile(f.path("config.json"))
				must(t, err)
				options.ExpectedObservation = review.Observation
				f.write("CLAUDE.md", "CANARY_NEW_EDIT")
			}
			var events []diagnostics.Event
			options.Observe = func(e diagnostics.Event) { events = append(events, e) }
			before := auditTree(t, f.dir)
			_, err := Apply(f.c, options)
			if !errors.Is(err, spec.sentinel) {
				t.Fatalf("wrong safety stop: %v", err)
			}
			if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
				t.Fatal("safety stop changed files")
			}
			found := false
			for _, event := range events {
				if event.Component == "bridge.transaction" && event.Code == spec.code {
					found = true
				}
			}
			if !found {
				t.Fatal("missing actionable transaction diagnostic", events)
			}
			data, err := json.Marshal(events)
			must(t, err)
			if strings.Contains(string(data), "CANARY") || strings.Contains(string(data), f.dir) {
				t.Fatal("safety diagnostic leaked private data")
			}
		})
	}
}

func TestReviewedOperationsReportConfigFailuresWithoutWrites(t *testing.T) {
	for _, name := range []string{"sync", "resolve", "restore"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.write("config.json", `{"version":1,"CANARY_SECRET":`)
			var events []diagnostics.Event
			sink := func(e diagnostics.Event) { events = append(events, e) }
			before := auditTree(t, f.dir)
			var err error
			observation := strings.Repeat("a", 64)
			switch name {
			case "sync":
				_, err = SyncReviewedObserved(f.path("config.json"), observation, sink)
			case "resolve":
				_, err = ResolveReviewedObserved(f.path("config.json"), observation, map[string]string{"rules": "claude"}, sink)
			case "restore":
				_, err = RestoreReviewedObserved(f.path("config.json"), observation, HistoryChoice{}, sink)
			}
			if err == nil {
				t.Fatal("malformed config accepted")
			}
			if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
				t.Fatal("config failure wrote files")
			}
			if len(events) != 1 || events[0].Stage != "config_load" || events[0].Path == "" || events[0].Code == "ok" {
				t.Fatal("config failure lost diagnostic location", events)
			}
			data, err := json.Marshal(events)
			must(t, err)
			if strings.Contains(string(data), "CANARY") || strings.Contains(string(data), f.dir) {
				t.Fatal("config diagnostic leaked raw details")
			}
		})
	}
}
