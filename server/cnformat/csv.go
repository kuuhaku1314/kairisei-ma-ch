// Package cnformat preserves shared original-client text format rules.
package cnformat

import "strings"

// SplitCSVLine follows the original Global.splitString(string,char,char): each
// quote toggles delimiter masking, quotes remain in the returned fields, and
// quote state never crosses a physical line. This is not RFC 4180 CSV.
func SplitCSVLine(line string) []string {
	line = strings.TrimRight(line, "\r")
	fields := make([]string, 0, 64)
	quoted, start := false, 0
	for index := 0; index < len(line); index++ {
		if line[index] == '"' {
			quoted = !quoted
		}
		if line[index] == ',' && !quoted {
			fields = append(fields, line[start:index])
			start = index + 1
		}
	}
	return append(fields, line[start:])
}
