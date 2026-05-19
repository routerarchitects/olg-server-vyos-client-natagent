package actions

import "errors"

const (
	ActionTrace = "trace"
)

var (
	ErrInvalidActionPayload = errors.New("invalid action payload")
)
