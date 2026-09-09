package bridge

import (
	"fmt"
	"path/filepath"
	"strings"
)

// EnrollmentDraft is deliberately not a loadable configuration. Selection is
// not compatibility consent; callers must review its Profile before using it.
type EnrollmentDraft struct {
	Format         string      `json:"format"`
	ReadOnly       bool        `json:"readOnly"`
	ReviewRequired bool        `json:"reviewRequired"`
	Profile        configInput `json:"profile"`
	Review         []string    `json:"review"`
}

// DraftProfile uses a fresh inventory, accepting only explicitly selected IDs.
// It never reads native contents, creates state or updates an ownership roster.
func DraftProfile(root, scope string, ids []string) (EnrollmentDraft, error) {
	draft := EnrollmentDraft{Format: "agent-bridge-enrollment-draft-v1", ReadOnly: true, ReviewRequired: true}
	if len(ids) == 0 {
		return draft, fmt.Errorf("select at least one discovered resource ID")
	}
	discovery, err := Discover(root, scope)
	if err != nil {
		return draft, err
	}
	selected := map[string]bool{}
	for _, id := range ids {
		key := strings.ToLower(id)
		if !safeID.MatchString(id) || selected[key] {
			return draft, fmt.Errorf("selection IDs must be unique and path-safe")
		}
		selected[key] = true
	}
	draft.Profile = configInput{Version: 1, StateDir: filepath.Join(discovery.Root, ".agent-bridge"), Resources: []resourceInput{}}
	for _, candidate := range discovery.Candidates {
		// Match exact spelling, but reject case-folded ambiguous inventory names.
		if !selected[strings.ToLower(candidate.ID)] {
			continue
		}
		for _, other := range discovery.Candidates {
			if candidate.ID != other.ID && strings.EqualFold(candidate.ID, other.ID) {
				return draft, fmt.Errorf("ambiguous case-colliding discovery candidates")
			}
		}
		for _, id := range ids {
			if candidate.ID == id {
				draft.Profile.Resources = append(draft.Profile.Resources, candidate)
				break
			}
		}
	}
	if len(draft.Profile.Resources) != len(ids) {
		return draft, fmt.Errorf("selection includes an absent or changed candidate; rediscover before reviewing")
	}
	draft.Review = []string{
		"This envelope is not a runnable profile. Selection does not grant portability, trust or permission to execute code.",
		"Review both native contents privately. This draft reads names and metadata only; it does not prove compatibility or pin content against later edits.",
		"Review stateDir before extracting profile to a NEW private config file. Never reuse another profile's state directory.",
		"Choose instruction-file and prepare marked sections if whole-file instruction synchronization is not intended.",
		"Set portable and allowReformat only after adapter review. Select MCP servers explicitly; credential stores and trust decisions must stay host-local.",
		"Run audit and plan on the reviewed profile, then check-overlap with every participating profile before enabling sync.",
		"With all participants stopped, review shared coordinationDir and roster membership. This command never enrolls or starts a watcher.",
	}
	return draft, nil
}
