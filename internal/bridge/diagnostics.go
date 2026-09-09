package bridge

import (
	"errors"
	"github.com/Jhorlin/agent-bridge/internal/diagnostics"
)

type Observer = diagnostics.Observer

var ErrRecoveryPending = errors.New("an interrupted transaction requires recover before syncing")
var ErrLockPresent = errors.New("another sync is active, or a stale sync.lock needs inspection")
var ErrConflicts = errors.New("conflicts block all writes")

func DiagnosticCode(err error) string {
	switch {
	case errors.Is(err, ErrObservationChanged):
		return "observation_changed"
	case errors.Is(err, ErrRecoveryPending):
		return "pending_recovery"
	case errors.Is(err, ErrLockPresent):
		return "lock_present"
	case errors.Is(err, ErrConflicts):
		return "conflict"
	default:
		return diagnostics.Code(err)
	}
}

// observe never passes raw identifiers or error messages to diagnostic storage.
func observe(sink diagnostics.Observer, stage, component, resource, path, transaction string, err error) {
	if sink == nil {
		return
	}
	code := DiagnosticCode(err)
	sink(diagnostics.Event{Stage: stage, Component: component, Code: code,
		Resource: diagnostics.Ref(resource), Path: diagnostics.Ref(path), Transaction: transaction})
}
