package errors

import "errors"

var (
	ErrNotLeader       = errors.New("not leader")
	ErrNodeNotFound    = errors.New("node not found")
	ErrAllocNotFound   = errors.New("allocation not found")
	ErrServiceNotFound = errors.New("service not found")
	ErrNoCapacity      = errors.New("no capacity available")
	ErrDraining        = errors.New("node is draining")
)
