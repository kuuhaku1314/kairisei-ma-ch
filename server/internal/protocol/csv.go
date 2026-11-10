// Package protocol preserves the CN client's wire and CSV contracts.
package protocol

import "kairisei.local/server/cnformat"

// CatalogRevision is the revision consumed by the client update handshake
// and the download catalog header. It is not account or master-data state.
const CatalogRevision = 790

// SplitCSVLine retains the protocol entry to the shared original-client parser.
func SplitCSVLine(line string) []string { return cnformat.SplitCSVLine(line) }
