package release

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Duration-based fuzz budgets can report the coordinator's deadline as a test
// failure (golang/go#75804). Keep finite execution budgets, a real hang timeout,
// and fail-fast semantics; never suppress or retry away a crashing counterexample.
func TestCIFuzzBudgetsAndFailureReporting(t *testing.T) {
	data, err := os.ReadFile("../../.github/workflows/test.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Run      string `yaml:"run"`
				Continue bool   `yaml:"continue-on-error"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	target := regexp.MustCompile(`-fuzz '\^([A-Za-z0-9]+)\$'`)
	for _, step := range workflow.Jobs["test"].Steps {
		if !strings.Contains(step.Run, "-fuzz ") {
			continue
		}
		match := target.FindStringSubmatch(step.Run)
		if len(match) != 2 || !strings.Contains(step.Run, "-fuzztime=100000x") || !strings.Contains(step.Run, "-timeout=2m") || !strings.Contains(step.Run, "-parallel=2") || step.Continue || strings.Contains(step.Run, "||") {
			t.Fatalf("unbounded, deadline-based or failure-suppressing fuzz step: %s", step.Run)
		}
		if seen[match[1]] {
			t.Fatalf("duplicate fuzz step %s", match[1])
		}
		seen[match[1]] = true
	}
	for _, name := range []string{"FuzzMCPJSON", "FuzzMCPTextPreservation", "FuzzPortableAgentRoundTrip", "FuzzStartupHookRoundTrip", "FuzzStrictSkillRoundTrip", "FuzzSkillInvocationRoundTrip", "FuzzPluginAgentRoundTrip"} {
		if !seen[name] {
			t.Fatalf("missing fuzz target %s", name)
		}
	}
}
