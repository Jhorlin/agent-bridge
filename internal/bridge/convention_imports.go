package bridge

import (
	"fmt"
	"regexp"
	"strings"
)

var instructionFence = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})(.*)$")

// Claude skips import expansion in Markdown fences. Keep ambiguous/unclosed
// fences blocked instead of allowing malformed markup to conceal an import.
func checkInstructionImports(text string) error {
	fence := ""
	for _, line := range strings.Split(text, "\n") {
		match := instructionFence.FindStringSubmatch(strings.TrimSuffix(line, "\r"))
		if fence != "" {
			if match != nil && match[1][0] == fence[0] && len(match[1]) >= len(fence) && strings.TrimSpace(match[2]) == "" {
				fence = ""
			}
			continue
		}
		if match != nil && (match[1][0] == '~' || !strings.Contains(match[2], "`")) {
			fence = match[1]
			continue
		}
		for _, word := range strings.Fields(withoutInlineCode(line)) {
			if strings.HasPrefix(word, "@") && len(word) > 1 {
				return fmt.Errorf("conventions: @ references require explicit instruction mapping or removal of host-specific imports")
			}
		}
	}
	if fence != "" {
		return fmt.Errorf("conventions: unterminated instruction code fence requires review")
	}
	return nil
}

// Recognize matched, single-line code spans only. Multiline and unmatched spans
// remain literal input to the conservative import guard.
var inlineBackticks = regexp.MustCompile("`+")

func withoutInlineCode(line string) string {
	runs := inlineBackticks.FindAllStringIndex(line, -1)
	next := make([]int, len(runs))
	last := map[int]int{}
	for i := len(runs) - 1; i >= 0; i-- {
		count := runs[i][1] - runs[i][0]
		next[i] = -1
		if j, ok := last[count]; ok {
			next[i] = j
		}
		last[count] = i
	}
	var out strings.Builder
	start := 0
	for i := 0; i < len(runs); {
		run := runs[i]
		out.WriteString(line[start:run[0]])
		escapes := 0
		for j := run[0] - 1; j >= 0 && line[j] == '\\'; j-- {
			escapes++
		}
		if escapes%2 == 1 || next[i] < 0 {
			out.WriteString(line[run[0]:run[1]])
			start = run[1]
			i++
			continue
		}
		// Pre-indexed matching delimiters keep malformed long lines linear.
		close := next[i]
		out.WriteByte(' ')
		start = runs[close][1]
		i = close + 1
	}
	out.WriteString(line[start:])
	return out.String()
}
