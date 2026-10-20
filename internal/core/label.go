package core

// Label is a process-level information-flow label. It does not describe bytes.
type Label string

const LabelSecret Label = "SECRET"
