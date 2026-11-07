package battlecodec

// InputCommandFieldCount is the self-hosted report envelope: HP, opcode and
// twenty int32 arguments. Card-selection records retain JP8's final summon mask.
// Original registration and executable-CRC rows are outside this envelope.
const InputCommandFieldCount = 22
