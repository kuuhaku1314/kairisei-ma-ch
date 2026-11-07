package battlemodel

// Result preserves the original battle command and signed integer argument
// layout. It is an engine output value; CSV framing and consumer presentation
// separators are owned by battlecodec. It carries no room or account metadata.
type Result struct {
	Command int
	Args    []int64
}
