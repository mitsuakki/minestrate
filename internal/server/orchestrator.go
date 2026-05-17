package server

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/google/uuid"
	"github.com/mitsuakki/minestrate/config"
	"github.com/docker/go-connections/nat"
)

type Orchestrator struct {
	cfg          *config.Config
	servers      map[string]*Server
	serversMutex sync.RWMutex
	ports        *PortAllocator
	networks     NetworkManager
	docker       DockerClient
	jobQueue     chan *Server
}

func NewOrchestrator(cfg *config.Config, docker DockerClient) (*Orchestrator, error) {
	var nm NetworkManager
	var err error

	mode := cfg.Network.Mode
	if mode == "" {
		mode = "simple"
	}

	switch mode {
	case "simple":
		if mode == "simple" {
			if err := EnsureNetwork(context.Background(), docker, cfg.Network.DefaultNetwork); err != nil {
				return nil, fmt.Errorf("failed to ensure network %q: %w", cfg.Network.DefaultNetwork, err)
			}
		}
		
		nm = NewSimpleNetworkManager(cfg.Network.DefaultNetwork)
	case "isolated":
		nm, err = NewIsolatedSubnetManager(docker, cfg.Network.SubnetBlock)
		if err != nil {
			return nil, err
		}
	default:
		return nil, ErrInvalidNetworkMode
	}

	if cfg.Network.EnableFallback && mode == "isolated" {
		secondary := NewSimpleNetworkManager(cfg.Network.DefaultNetwork)
		nm = NewFallbackNetworkManager(nm, secondary)
	}

	o := &Orchestrator{
		cfg:      cfg,
		servers:  make(map[string]*Server),
		ports:    NewPortAllocator(cfg.Ports.RangeStart, cfg.Ports.RangeEnd),
		networks: nm,
		docker:   docker,
		jobQueue: make(chan *Server, cfg.Orchestrator.Workers),
	}

	return o, nil
}

func (o *Orchestrator) CreateServer(ctx context.Context, game string, players int) (*Server, error) {
	o.serversMutex.Lock()
	if len(o.servers) >= o.cfg.Orchestrator.MaxServers {
		o.serversMutex.Unlock()
		return nil, ErrMaxServersReached
	}

	port, err := o.ports.Acquire()
	if err != nil {
		o.serversMutex.Unlock()
		return nil, err
	}

	id := uuid.New().String()

	netCfg, err := o.networks.Allocate(ctx, id)
	if err != nil {
		o.ports.Release(port)
		o.serversMutex.Unlock()
		return nil, err
	}

	s := NewServer(id, game, players, "127.0.0.1", port)
	s.Network = netCfg

	o.servers[id] = s
	o.serversMutex.Unlock()

	fmt.Printf("About to send job %s to queue (len=%d cap=%d)\n", s.ID, len(o.jobQueue), cap(o.jobQueue))
	select {
	case o.jobQueue <- s:
		fmt.Printf("Job sent: %s\n", id)
		return s, nil
	case <-ctx.Done():
		// Cleanup if queue is full
		o.serversMutex.Lock()
		delete(o.servers, id)
		o.serversMutex.Unlock()
		o.ports.Release(port)
		_ = o.networks.Release(ctx, id)
		return nil, ErrJobQueueFull
	}
}

func (o *Orchestrator) StopServer(ctx context.Context, id string) error {
	o.serversMutex.Lock()
	defer o.serversMutex.Unlock()

	s, ok := o.servers[id]
	if !ok {
		return ErrServerNotFound
	}

	if err := s.Transition(EventStop); err != nil {
		return err
	}

	delete(o.servers, id)
	o.ports.Release(s.Port)
	return o.networks.Release(ctx, id)
}

func (o *Orchestrator) ShutdownServer(ctx context.Context, id string) error {
	o.serversMutex.RLock()
	s, ok := o.servers[id]
	o.serversMutex.RUnlock()

	if !ok {
		return ErrServerNotFound
	}

	if s.State() != StateRunning {
		return ErrServerNotRunning
	}

	if err := s.Transition(EventDrain); err != nil {
		return err
	}

	go func() {
		// Use background context for cleanup to ensure it completes even if request ctx is canceled
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		_ = o.docker.ContainerStop(cleanupCtx, s.ID, container.StopOptions{})
		
		o.ports.Release(s.Port)
		_ = o.networks.Release(cleanupCtx, s.ID)
		
		_ = s.Transition(EventStop)
	}()

	return nil
}

func (o *Orchestrator) GC() {
	o.serversMutex.Lock()
	defer o.serversMutex.Unlock()

	for id, s := range o.servers {
		if s.State() == StateStopped {
			delete(o.servers, id)
		}
	}
}

func (o *Orchestrator) StartGC(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			o.GC()
		}
	}()
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
	fmt.Printf("Worker %d started\n", id)
	for s := range o.jobQueue {
		fmt.Printf("Worker %d starting server %s\n", id, s.ID)
		
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(o.cfg.Orchestrator.StartTimeout)*time.Second)
		
		err := o.processJob(ctx, s)
		cancel()
		fmt.Printf("processJob result for %s: %v\n", s.ID, err)

		if err != nil {
			fmt.Printf("Worker %d failed to start server %s: %v\n", id, s.ID, err)
			_ = s.Transition(EventStop)
			// Resource cleanup is handled in StopServer if called by user, 
			// but here we might need to remove from orchestrator map if it failed during startup
			// and wasn't yet "Running".
			o.serversMutex.Lock()
			delete(o.servers, s.ID)
			o.ports.Release(s.Port)
			_ = o.networks.Release(context.Background(), s.ID)
			o.serversMutex.Unlock()
		}
	}
}

func (o *Orchestrator) processJob(ctx context.Context, s *Server) error {
	if err := s.Transition(EventStart); err != nil {
		return err
	}

	// Create container
	fmt.Printf("Attempting to create container for server %s with image %s\n", s.ID, o.cfg.Docker.Image)
	resp, err := o.docker.ContainerCreate(ctx, &container.Config{
		Image: o.cfg.Docker.Image,
		Labels: map[string]string{
			"minestrate.server_id": s.ID,
		},
	}, &container.HostConfig{
		NetworkMode: container.NetworkMode(s.Network.NetworkName),
		PortBindings: nat.PortMap{
			nat.Port("19132/udp"): []nat.PortBinding{
				{
					HostIP:   "0.0.0.0",
					HostPort: fmt.Sprintf("%d", s.Port),
				},
			},
		},
	}, nil, nil, s.ID)
	fmt.Printf("ContainerCreate resp: %+v, err: %v\n", resp, err)

	if err != nil {
		fmt.Printf("Failed to create container for server %s: %v\n", s.ID, err)
		return err
	}
	fmt.Printf("Successfully created container %s for server %s\n", resp.ID, s.ID)

	// Start container
	if err := o.docker.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		fmt.Printf("ContainerStart err: %v\n", err)
		return err
	}
	fmt.Printf("ContainerStart err: %v\n", err)

	return s.Transition(EventRun)
}
