package cli

import (
	"bytes"
	"context"
	"io"
	"testing"
)

func TestVersion(t *testing.T) {
	for _, arg := range []string{"--version", "version"} {
		var out bytes.Buffer
		if Run(context.Background(), []string{arg}, &out, io.Discard) != 0 || out.String() != "agent-bridge dev\n" {
			t.Fatal("version command failed")
		}
	}
	if Run(context.Background(), []string{"--version"}, &auditFailWriter{}, io.Discard) != 1 {
		t.Fatal("ignored output error")
	}
}
