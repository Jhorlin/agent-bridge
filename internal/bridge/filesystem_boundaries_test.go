package bridge

import (
	"os"
	"reflect"
	"testing"
)

func TestSnapshotWriteRejectsInvalidInputWithoutTouchingTarget(t *testing.T) {
	for _, bad := range []*Snapshot{nil, {Data: "not base64", Mode: 0600}, {Data: "bmV3", Mode: 01000}} {
		f := newFixture(t)
		f.write("target", "preserve")
		before := auditTree(t, f.dir)
		if err := writeSnapshot(f.path("target"), bad); err == nil {
			t.Fatal("invalid snapshot accepted")
		}
		if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
			t.Fatal("invalid input touched target or left a temporary file")
		}
	}
}

func TestSnapshotWriteRejectsUnsafeDestinationsAndCleansUp(t *testing.T) {
	for _, kind := range []string{"relative", "linked-parent", "file-parent", "directory-target"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			path := f.path("target")
			switch kind {
			case "relative":
				t.Chdir(f.dir)
				path = "never-create-this-relative-target"
			case "linked-parent":
				f.write("real/target", "preserve")
				must(t, os.Symlink(f.path("real"), f.path("alias")))
				path = f.path("alias/target")
			case "file-parent":
				f.write("parent", "preserve")
				path = f.path("parent/target")
			case "directory-target":
				f.write("target/child", "preserve")
			}
			before := auditTree(t, f.dir)
			if err := writeSnapshot(path, &Snapshot{Data: "bmV3", Mode: 0600}); err == nil {
				t.Fatal("unsafe destination accepted")
			}
			if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
				t.Fatal("failed write altered destination or left temporary files")
			}
		})
	}
}

func TestSnapshotWriteRoundTripsEmptyAndExecutableFiles(t *testing.T) {
	f := newFixture(t)
	for _, spec := range []struct {
		name, data, text string
		mode             uint32
	}{
		{"empty", "", "", 0600}, {"nested/script", "aGVsbG8K", "hello\n", 0700},
	} {
		must(t, writeSnapshot(f.path(spec.name), &Snapshot{Data: spec.data, Mode: spec.mode}))
		f.expect(spec.name, spec.text)
		actual, err := snapshot(f.path(spec.name))
		must(t, err)
		if actual == nil || actual.Data != spec.data || actual.Mode != spec.mode {
			t.Fatal("snapshot round trip lost bytes or permissions")
		}
	}
}
