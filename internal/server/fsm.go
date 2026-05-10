package server

import (
	"fmt"
	"sync"
)

// ServerState represents the lifecycle state of a server
type ServerState string

const (
	StatePending  ServerState = "pending"
	StateStarting ServerState = "starting"
	StateRunning  ServerState = "running"
	StateDraining ServerState = "draining"
	StateStopped  ServerState = "stopped"
)

// ServerEvent representsa trigger that cause a transition
type ServerEvent string

const (
	EventStart   ServerEvent = "start"
	EventRun     ServerEvent = "run"
	EventDrain   ServerEvent = "drain"
	EventStop    ServerEvent = "stop"
	EventTimeout ServerEvent = "timeout"
)

type ErrInvalidTransition struct {
	From  ServerState
	Event ServerEvent
}

func (e *ErrInvalidTransition) Error() string {
	return fmt.Sprintf("invalid transaction from=%s event=%s", e.From, e.Event)
}

type Server struct {
	mutex sync.RWMutex
	state ServerState
}

func NewServer() *Server {
	return &Server{
		state: StatePending,
	}
}

func (s *Server) State() ServerState {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.state
}

func (s *Server) Transition(event ServerEvent) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	switch s.state {
		case StatePending:
			if event == EventStart {
				s.state = StateStarting
				return nil
			}

		case StateStarting:
			if event == EventRun {
				s.state = StateRunning
				return nil
			}
			if event == EventTimeout {
				s.state = StateStopped
				return nil
			}
		case StateRunning:
			if event == EventDrain {
				s.state = StateDraining
				return nil
			}
		case StateDraining:
			if event == EventStop {
				s.state = StateStopped
				return nil
			}
		case StateStopped:
			// Terminal state: no outbound transitions permitted.
	}

	return &ErrInvalidTransition{
		From:  s.state,
		Event: event,
	}
}