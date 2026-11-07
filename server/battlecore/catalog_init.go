package battlecore

// NewCatalog validates a completed content projection before it is shared with
// battles. Callers retain ownership and must keep definitions immutable while
// the catalog is in use; loading files and authoring edits belong to adapters.
func NewCatalog(definitions *Definitions) (*CombatCatalog, error) {
	catalog := (*CombatCatalog)(definitions)
	if err := catalog.Validate(); err != nil {
		return nil, err
	}
	return catalog, nil
}
