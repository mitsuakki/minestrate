package server

import "errors"

var (
	ErrMaxServersReached = errors.New("max servers reached")
	ErrNoPortsAvailable  = errors.New("no ports available")
	ErrNoSubnetsAvailable = errors.New("no subnets available")
	ErrJobQueueFull      = errors.New("job queue full")
)
