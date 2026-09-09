package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/Jhorlin/agent-bridge/internal/bridge"
)

func runEnrollmentCreation(args []string, out, errOut io.Writer) int {
	var result any
	var err error
	switch args[0] {
	case "review-enrollment":
		if len(args) != 3 {
			return usage(errOut)
		}
		result, err = bridge.ReviewEnrollmentCreation(args[1], args[2])
	case "create-enrolled":
		if len(args) != 4 {
			return usage(errOut)
		}
		err = bridge.CreateEnrolledReviewed(args[1], args[2], args[3])
		result = map[string]string{"status": "created-and-enrolled", "nativeFiles": "unchanged"}
	case "recover-enrollment":
		if len(args) != 3 {
			return usage(errOut)
		}
		err = bridge.RecoverEnrollmentCreation(args[1], args[2])
		result = map[string]string{"status": "recovery-complete", "note": "Any recorded new profile creation was rolled back; its private backup is retained."}
	}
	if err != nil {
		if errors.Is(err, bridge.ErrObservationChanged) {
			fmt.Fprintln(errOut, "Enrollment inputs changed; review again.")
			return 2
		}
		fmt.Fprintln(errOut, "Enrollment operation failed; inspect the template, target, coordinator, ownership and enrollment recovery journal privately.")
		return 1
	}
	if err := json.NewEncoder(out).Encode(result); err != nil {
		return 1
	}
	return 0
}
