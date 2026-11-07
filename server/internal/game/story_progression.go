package game

// The captured StoryMainShow exposes part 3 (Alchemy) while part 1 is still
// unfinished. Parts 1 and 2 retain their existing sequential progression.
func MainStoryProgressionChain(partID int) int {
	if partID == 3 {
		return 3
	}
	return 0
}
