package bridge

import (
	"encoding/base64"
	"testing"
)

func FuzzPortableAgentRoundTrip(f *testing.F) {
	f.Add("claude", "---\nname: reviewer\ndescription: Review code.\n---\nInspect changes.")
	f.Add("claude", "---\nname: reviewer\ndescription: !!binary /Q==\n---\nInspect changes.")
	f.Add("codex", "name='reviewer'\ndescription='Review code.'\ndeveloper_instructions='Inspect changes.'\n")
	f.Add("shared", `{"name":"reviewer","description":"Review code.","developer_instructions":"Inspect changes."}`)
	f.Fuzz(func(t *testing.T, side, input string) {
		if len(input) > 64*1024 || (side != "claude" && side != "codex" && side != "shared") {
			return
		}
		raw := &Snapshot{Data: base64.StdEncoding.EncodeToString([]byte(input)), Mode: 0600}
		common, err := normalizeAgent(side, raw)
		if err != nil {
			return
		}
		rendered, err := renderAgent(side, common)
		if err != nil {
			t.Fatal("accepted agent failed rendering")
		}
		again, err := normalizeAgent(side, rendered)
		if err != nil || fingerprint(common) != fingerprint(again) {
			t.Fatal("accepted agent did not round-trip")
		}
	})
}

func FuzzStartupHookRoundTrip(f *testing.F) {
	f.Add(`{"hooks":{"SessionStart":[{"matcher":"^startup$","hooks":[{"type":"command","command":"/usr/bin/true","timeout":10}]}]}}`)
	f.Add(`{"hooks":{"Stop":[]}}`)
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 64*1024 {
			return
		}
		raw := &Snapshot{Data: base64.StdEncoding.EncodeToString([]byte(input)), Mode: 0600}
		common, err := normalizeHooks("claude", raw)
		// Missing/empty hooks represent absence, not renderable shared content.
		if err != nil || common == nil {
			return
		}
		rendered, err := renderHooks("codex", common, raw)
		if err != nil {
			t.Fatal("accepted hook failed rendering")
		}
		again, err := normalizeHooks("codex", rendered)
		if err != nil || fingerprint(common) != fingerprint(again) {
			t.Fatal("accepted hook did not round-trip")
		}
	})
}
