package bridge

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/Jhorlin/agent-bridge/internal/diagnostics"
)

func TestObservedWriteFailureIdentifiesTransactionAndPreservesRollback(t *testing.T) {
	f := newFixture(t)
	f.apply()
	f.write("CLAUDE.md", "CANARY_CONTENT")
	var events []diagnostics.Event
	_, err := Apply(f.c, Options{Observe: func(e diagnostics.Event) { events = append(events, e) }, BeforeWrite: func(_ int, op Operation) error {
		if op.File == f.path("AGENTS.md") {
			return &os.PathError{Op: "CANARY_OPERATION", Path: "CANARY_PASSWORD", Err: syscall.ENOSPC}
		}
		return nil
	}})
	if err == nil {
		t.Fatal("injected failure did not fire")
	}
	f.expect("AGENTS.md", "one")
	f.expect("state/shared/rules", "one")
	f.expect("CLAUDE.md", "CANARY_CONTENT")
	failed, rolled := false, false
	tx := ""
	for _, e := range events {
		if e.Stage == "write" && e.Code == "storage_full" {
			failed = true
			tx = e.Transaction
			if e.Path != diagnostics.Ref(f.path("AGENTS.md")) || e.Resource != diagnostics.Ref("rules") || tx == "" || e.Component != "bridge.transaction" {
				t.Fatal("failure lacks useful context", e)
			}
		}
		if e.Stage == "rollback" && e.Code == "ok" {
			rolled = true
			if tx != "" && e.Transaction != tx {
				t.Fatal("lost transaction correlation")
			}
		}
	}
	if !failed || !rolled {
		t.Fatalf("missing events: %+v", events)
	}
	b, _ := json.Marshal(events)
	if strings.Contains(string(b), "CANARY") || strings.Contains(string(b), f.dir) {
		t.Fatal("diagnostic leaked raw data")
	}
}
func TestObservedRecoveryIncludesTransaction(t *testing.T) {
	f, pointer, _, _ := interruptedReplacement(t)
	var events []diagnostics.Event
	r, err := RecoverObserved(f.c, func(e diagnostics.Event) { events = append(events, e) })
	must(t, err)
	if r.Status != "recovered" || len(events) != 1 || events[0].Transaction != pointer.Transaction || events[0].Code != "ok" {
		t.Fatal("missing recovery event", events)
	}
}
func TestObservedPlanFailureAndLockAreSafe(t *testing.T) {
	f := newFixture(t)
	f.raw.Resources[0].Kind = "agent-file"
	f.raw.Resources[0].Portable = true
	f.raw.Resources[0].AllowReformat = true
	f.raw.Resources[0].ID = "CANARY_RESOURCE"
	f.load()
	before := auditTree(t, f.dir)
	var events []diagnostics.Event
	_, err := PlanObserved(f.c, func(e diagnostics.Event) { events = append(events, e) })
	if err == nil || len(events) != 1 || events[0].Stage != "normalize" || events[0].Resource != diagnostics.Ref("CANARY_RESOURCE") {
		t.Fatalf("missing adapter location: %v %+v", err, events)
	}
	if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
		t.Fatal("observed plan wrote files")
	}
	f = newFixture(t)
	f.write("state/sync.lock", "CANARY_LOCK_CONTENT")
	events = nil
	_, err = Apply(f.c, Options{Observe: func(e diagnostics.Event) { events = append(events, e) }})
	if !errors.Is(err, ErrLockPresent) || len(events) != 1 || events[0].Stage != "lock" || events[0].Code != "lock_present" {
		t.Fatal("lock not diagnosed", events)
	}
}
