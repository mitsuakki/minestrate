package server

import (
	"sync/atomic"
)

type PortAllocator struct {
	rangeStart int
	rangeEnd   int
	bits       []uint64
}

func NewPortAllocator(start, end int) *PortAllocator {
	if start > end {
		panic("invalid port range: start > end")
	}
	count := end - start + 1
	numUint64 := (count + 63) / 64
	bits := make([]uint64, numUint64)

	// Mark bits beyond rangeEnd as reserved in the last uint64
	if count % 64 != 0 {
		lastIdx := numUint64 - 1
		remainingBits := count % 64
		// Bits from remainingBits to 63 should be set to 1
		var mask uint64 = ^((1 << remainingBits) - 1)
		bits[lastIdx] = mask
	}

	return &PortAllocator{
		rangeStart: start,
		rangeEnd:   end,
		bits:       bits,
	}
}

func (a *PortAllocator) Acquire() (int, error) {
	for i := 0; i < len(a.bits); i++ {
		for {
			val := atomic.LoadUint64(&a.bits[i])
			if val == ^uint64(0) {
				break // This uint64 is full, move to next
			}

			// Find first zero bit
			bitIdx := -1
			for j := 0; j < 64; j++ {
				if (val & (1 << j)) == 0 {
					bitIdx = j
					break
				}
			}

			if bitIdx == -1 {
				break // Should not happen if val != ^uint64(0)
			}

			newVal := val | (1 << bitIdx)
			if atomic.CompareAndSwapUint64(&a.bits[i], val, newVal) {
				port := a.rangeStart + i*64 + bitIdx
				return port, nil
			}
			// CAS failed, retry this uint64
		}
	}
	return 0, ErrNoPortsAvailable
}

func (a *PortAllocator) Release(port int) {
	if port < a.rangeStart || port > a.rangeEnd {
		return
	}

	offset := port - a.rangeStart
	idx := offset / 64
	bitIdx := offset % 64
	mask := uint64(1) << bitIdx

	for {
		val := atomic.LoadUint64(&a.bits[idx])
		if (val & mask) == 0 {
			return // Already released
		}
		newVal := val & ^mask
		if atomic.CompareAndSwapUint64(&a.bits[idx], val, newVal) {
			return
		}
	}
}
