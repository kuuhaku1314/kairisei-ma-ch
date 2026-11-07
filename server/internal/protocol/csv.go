// Package protocol preserves the CN client's wire and CSV contracts.
package protocol

import "kairisei.local/server/cnformat"

// SplitCSVLine retains the protocol entry to the shared original-client parser.
func SplitCSVLine(line string) []string { return cnformat.SplitCSVLine(line) }
