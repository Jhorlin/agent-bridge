package bridge

import "fmt"

type ReviewCheckpoint struct {
	Version     int       `json:"version"`
	ReadOnly    bool      `json:"readOnly"`
	Observation string    `json:"observation"`
	Summaries   []Summary `json:"summaries"`
}

// ReviewProfile reads the managed inputs but emits no raw configuration values.
// The observation is a freshness check, not proof of human review or host trust.
func ReviewProfile(filename string) (ReviewCheckpoint, error) {
	r := ReviewCheckpoint{Version: 1, ReadOnly: true}
	c, err := LoadAuditConfig(filename)
	if err != nil {
		return r, fmt.Errorf("review profile is invalid or unsafe")
	}
	p, err := Plan(c)
	if err != nil {
		return r, fmt.Errorf("review planning failed; audit the profile privately")
	}
	if p.HasConflicts() {
		return r, fmt.Errorf("conflicts block review; inspect the plan")
	}
	r.Observation = Observation(c, p)
	r.Summaries = p.Summaries()
	return r, nil
}

// SyncReviewed reuses the transaction engine's under-lock observation check.
// Invalid/empty tokens must never fall back to unconditional Apply.
func SyncReviewed(filename, observation string) ([]Summary, error) {
	if !digestPattern.MatchString(observation) {
		return nil, fmt.Errorf("expected a review observation digest")
	}
	c, err := LoadAuditConfig(filename)
	if err != nil {
		return nil, fmt.Errorf("reviewed profile is invalid or unsafe")
	}
	return Apply(c, Options{ExpectedObservation: observation})
}
