package server

import (
	"sync"
	"testing"
)

func TestPortAllocator_Concurrency(t *testing.T) {
	start, end := 20000, 30000
	alloc := NewPortAllocator(start, end)

	const numWorkers = 1000
	ports := make(chan int, numWorkers)
	var wg sync.WaitGroup

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			port, err := alloc.Acquire()
			if err != nil {
				t.Errorf("Acquire failed: %v", err)
				return
			}
			ports <- port
		}()
	}

	wg.Wait()
	close(ports)

	seen := make(map[int]bool)
	for port := range ports {
		if seen[port] {
			t.Errorf("Duplicate port allocated: %d", port)
		}
		seen[port] = true
		if port < start || port > end {
			t.Errorf("Port out of range: %d", port)
		}
	}

	if len(seen) != numWorkers {
		t.Errorf("Expected %d unique ports, got %d", numWorkers, len(seen))
	}
}

func TestPortAllocator_Exhaustion(t *testing.T) {
	start, end := 20000, 20005
	alloc := NewPortAllocator(start, end)

	for i := 0; i < 6; i++ {
		_, err := alloc.Acquire()
		if err != nil {
			t.Fatalf("Expected port, got error: %v", err)
		}
	}

	_, err := alloc.Acquire()
	if err != ErrNoPortsAvailable {
		t.Errorf("Expected ErrNoPortsAvailable, got %v", err)
	}
}

func TestPortAllocator_Release(t *testing.T) {
	start, end := 20000, 20000
	alloc := NewPortAllocator(start, end)

	port, err := alloc.Acquire()
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	alloc.Release(port)
	// Idempotency check
	alloc.Release(port)

	port2, err := alloc.Acquire()
	if err != nil {
		t.Fatalf("Acquire failed after release: %v", err)
	}
	if port != port2 {
		t.Errorf("Expected same port to be reused, got %d", port2)
	}
}
