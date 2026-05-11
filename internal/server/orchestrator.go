package server

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mitsuakki/minestrate/config"
)

type Orchestrator struct {
	cfg           *config.Config
	servers       map[string]*Server
	serversMutex  sync.RWMutex
	availablePorts []int
	portsMutex    sync.Mutex
	jobQueue      chan *Server
}

func NewOrchestrator(cfg *config.Config) *Orchestrator {
	o := &Orchestrator{
		cfg:      cfg,
		servers:  make(map[string]*Server),
		jobQueue: make(chan *Server, 100),
	}

	// Initialize ports
	for i := cfg.Ports.RangeStart; i <= cfg.Ports.RangeEnd; i++ {
		o.availablePorts = append(o.availablePorts, i)
	}

	return o
}

func (o *Orchestrator) CreateServer(game string, players int) (*Server, error) {
	o.serversMutex.Lock()
	if len(o.servers) >= o.cfg.Orchestrator.MaxServers {
		o.serversMutex.Unlock()
		return nil, errors.New("max servers reached")
	}
	o.serversMutex.Unlock()

	port, err := o.reservePort()
	if err != nil {
		return nil, err
	}

	id := uuid.New().String()
	s := NewServer(id, game, players, "127.0.0.1", port)

	o.serversMutex.Lock()
	o.servers[id] = s
	o.serversMutex.Unlock()

	o.jobQueue <- s

	return s, nil
}

func (o *Orchestrator) reservePort() (int, error) {
	o.portsMutex.Lock()
	defer o.portsMutex.Unlock()

	if len(o.availablePorts) == 0 {
		return 0, errors.New("no ports available")
	}

	port := o.availablePorts[0]
	o.availablePorts = o.availablePorts[1:]
	return port, nil
}

func (o *Orchestrator) GetServer(id string) (*Server, bool) {
	o.serversMutex.RLock()
	defer o.serversMutex.RUnlock()
	s, ok := o.servers[id]
	return s, ok
}

func (o *Orchestrator) ListServers() []*Server {
	o.serversMutex.RLock()
	defer o.serversMutex.RUnlock()
	list := make([]*Server, 0, len(o.servers))
	for _, s := range o.servers {
		list = append(list, s)
	}
	return list
}

func (o *Orchestrator) StartWorkers() {
	for i := 0; i < o.cfg.Orchestrator.Workers; i++ {
		go o.worker(i)
	}
}

func (o *Orchestrator) worker(id int) {
	for s := range o.jobQueue {
		fmt.Printf("Worker %d starting server %s\n", id, s.ID)
		_ = s.Transition(EventStart)
		
		// Simulate startup
		time.Sleep(100 * time.Millisecond)
		_ = s.Transition(EventRun)
	}
}
