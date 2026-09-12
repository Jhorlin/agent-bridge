package bridge

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestWatchIntervalProfileValidationAndFlattening(t *testing.T) {
	f := newFixture(t)
	for _, value := range []string{"-1", "3601", "1.5", `"30"`} {
		f.write("interval.json", `{"version":1,"stateDir":"state","resources":[],"watchIntervalSeconds":`+value+`}`)
		if _, err := LoadAuditConfig(f.path("interval.json")); err == nil {
			t.Fatalf("accepted interval %s", value)
		}
	}
	f.write("interval.json", `{"version":1,"stateDir":"state","resources":[],"watchIntervalSeconds":30}`)
	c, err := LoadAuditConfig(f.path("interval.json"))
	must(t, err)
	flat, err := flatProfile(c)
	must(t, err)
	b, err := snapshotBytes(flat)
	must(t, err)
	var fields map[string]any
	must(t, json.Unmarshal(b, &fields))
	if fields["watchIntervalSeconds"] != float64(30) {
		t.Fatal("flattening lost polling policy")
	}
	f.write("child.json", `{"version":1,"stateDir":"child-state","resources":[],"extends":"interval.json"}`)
	child, err := LoadAuditConfig(f.path("child.json"))
	must(t, err)
	encoded, err := json.Marshal(child)
	must(t, err)
	if !strings.Contains(string(encoded), `"watchIntervalSeconds":30`) {
		t.Fatal("child lost inherited interval")
	}
}

func TestWatchIntervalBoundariesAndInheritance(t *testing.T) {
	f := newFixture(t)
	for _, value := range []int{0, 1, 3600} {
		f.write("interval.json", fmt.Sprintf(`{"version":1,"stateDir":"state","resources":[],"watchIntervalSeconds":%d}`, value))
		c, err := LoadAuditConfig(f.path("interval.json"))
		must(t, err)
		if c.WatchIntervalSeconds != value {
			t.Fatalf("interval %d resolved as %d", value, c.WatchIntervalSeconds)
		}
	}
	f.write("parent.json", `{"version":1,"stateDir":"state","resources":[],"watchIntervalSeconds":30}`)
	for _, tc := range []struct {
		field string
		want  int
	}{
		{"", 30}, {`,"watchIntervalSeconds":0`, 30}, {`,"watchIntervalSeconds":1`, 1},
	} {
		f.write("child.json", `{"version":1,"stateDir":"child-state","resources":[],"extends":"parent.json"`+tc.field+`}`)
		c, err := LoadAuditConfig(f.path("child.json"))
		must(t, err)
		if c.WatchIntervalSeconds != tc.want {
			t.Fatalf("child %s: got %d, want %d", tc.field, c.WatchIntervalSeconds, tc.want)
		}
	}
	f.write("parent.json", `{"version":1,"stateDir":"state","resources":[],"watchIntervalSeconds":-1}`)
	if _, err := LoadAuditConfig(f.path("child.json")); err == nil {
		t.Fatal("child override concealed invalid parent")
	}
}

func TestWatchIntervalSurvivesEnrollment(t *testing.T) {
	f := creationFixture(t)
	f.raw.WatchIntervalSeconds = 30
	f.load()
	target := f.path("registered.json")
	r, err := ReviewEnrollmentCreation(f.path("config.json"), target)
	must(t, err)
	must(t, CreateEnrolledReviewed(f.path("config.json"), target, r.Observation))
	c, err := LoadAuditConfig(target)
	must(t, err)
	if c.WatchIntervalSeconds != 30 {
		t.Fatal("enrollment lost polling interval")
	}
}
