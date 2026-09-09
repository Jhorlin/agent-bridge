package bridge

import (
	"errors"
	"testing"
)

func TestFollowupReviewRollbackPreservesUnattemptedReplacement(t *testing.T) {
	f := newFixture(t)
	f.apply()
	f.write("CLAUDE.md", "new version")
	target := f.path("AGENTS.md")
	var external *Snapshot
	_, err := Apply(f.c, Options{BeforeWrite: func(_ int, op Operation) error {
		if op.File == target {
			if op.Before == nil {
				t.Fatal("expected an existing destination")
			}
			external = op.After
			must(t, writeSnapshot(target, external))
			return errors.New("other editor saved before this write")
		}
		return nil
	}})
	if err == nil || external == nil {
		t.Fatal("probe did not reach external replacement")
	}
	current, err := snapshot(target)
	must(t, err)
	if !equal(current, external) {
		t.Fatal("rollback reverted an external edit to an existing file the bridge never wrote")
	}
	_, err = Recover(f.c)
	contains(t, err, "replacement ownership")
	current, err = snapshot(target)
	must(t, err)
	if !equal(current, external) {
		t.Fatal("recovery retry reverted the external edit")
	}
}
