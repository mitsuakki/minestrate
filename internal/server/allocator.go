package server

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"

	"github.com/docker/docker/api/types/network"
)

type DockerClient interface {
	NetworkCreate(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error)
	NetworkRemove(ctx context.Context, networkID string) error
}

type PortAllocator struct {
	rangeStart int
	rangeEnd   int
	bits       []uint64
}

type NetworkAllocator struct {
	docker      DockerClient
	baseSubnet  *net.IPNet
	subnets     []*net.IPNet
	subnetToIdx map[string]int
	bits        []uint64
}

func NewNetworkAllocator(docker DockerClient, subnetBlock string) (*NetworkAllocator, error) {
	_, ipnet, err := net.ParseCIDR(subnetBlock)
	if err != nil {
		return nil, err
	}

	subnets, err := partitionSubnet(ipnet, 28)
	if err != nil {
		return nil, err
	}

	numUint64 := (len(subnets) + 63) / 64
	bits := make([]uint64, numUint64)

	subnetToIdx := make(map[string]int)
	for i, s := range subnets {
		subnetToIdx[s.String()] = i
	}

	// Mark bits beyond len(subnets) as reserved
	if len(subnets)%64 != 0 {
		lastIdx := numUint64 - 1
		remainingBits := len(subnets) % 64
		var mask uint64 = ^((1 << remainingBits) - 1)
		bits[lastIdx] = mask
	}

	return &NetworkAllocator{
		docker:      docker,
		baseSubnet:  ipnet,
		subnets:     subnets,
		subnetToIdx: subnetToIdx,
		bits:        bits,
	}, nil
}

func (a *NetworkAllocator) Acquire(ctx context.Context) (string, error) {
	for i := 0; i < len(a.bits); i++ {
		for {
			val := atomic.LoadUint64(&a.bits[i])
			if val == ^uint64(0) {
				break
			}

			bitIdx := -1
			for j := 0; j < 64; j++ {
				if (val & (1 << j)) == 0 {
					bitIdx = j
					break
				}
			}

			if bitIdx == -1 {
				break
			}

			newVal := val | (1 << bitIdx)
			if atomic.CompareAndSwapUint64(&a.bits[i], val, newVal) {
				idx := i*64 + bitIdx
				subnet := a.subnets[idx].String()
				name := fmt.Sprintf("minestrate-net-%d", idx)

				_, err := a.docker.NetworkCreate(ctx, name, network.CreateOptions{
					Driver: "bridge",
					IPAM: &network.IPAM{
						Config: []network.IPAMConfig{
							{
								Subnet: subnet,
							},
						},
					},
				})
				if err != nil {
					// Release bit on failure
					a.releaseIndex(idx)
					return "", err
				}

				return subnet, nil
			}
		}
	}
	return "", ErrNoSubnetsAvailable
}

func (a *NetworkAllocator) Release(ctx context.Context, subnet string) error {
	idx, ok := a.subnetToIdx[subnet]
	if !ok {
		return nil
	}

	name := fmt.Sprintf("minestrate-net-%d", idx)
	if err := a.docker.NetworkRemove(ctx, name); err != nil {
		return err
	}

	a.releaseIndex(idx)
	return nil
}

func (a *NetworkAllocator) releaseIndex(idx int) {
	i := idx / 64
	bitIdx := idx % 64
	mask := uint64(1) << bitIdx

	for {
		val := atomic.LoadUint64(&a.bits[i])
		if (val & mask) == 0 {
			return
		}
		newVal := val & ^mask
		if atomic.CompareAndSwapUint64(&a.bits[i], val, newVal) {
			return
		}
	}
}

func partitionSubnet(base *net.IPNet, newMask int) ([]*net.IPNet, error) {
	ones, bits := base.Mask.Size()
	if newMask < ones {
		return nil, fmt.Errorf("new mask %d must be greater than or equal to base mask %d", newMask, ones)
	}

	numSubnets := 1 << (newMask - ones)
	subnets := make([]*net.IPNet, 0, numSubnets)

	currentIP := make(net.IP, len(base.IP))
	copy(currentIP, base.IP)

	// Subnet size is 2^(bits - newMask)
	// For IPv4, bits is 32. If newMask is 28, size is 2^4 = 16.
	subnetSize := 1 << (bits - newMask)

	for i := 0; i < numSubnets; i++ {
		subnetIP := make(net.IP, len(currentIP))
		copy(subnetIP, currentIP)
		subnets = append(subnets, &net.IPNet{
			IP:   subnetIP,
			Mask: net.CIDRMask(newMask, bits),
		})

		incrementIP(currentIP, subnetSize)
	}

	return subnets, nil
}

func incrementIP(ip net.IP, inc int) {
	for i := len(ip) - 1; i >= 0; i-- {
		sum := int(ip[i]) + inc
		ip[i] = byte(sum)
		inc = sum >> 8
		if inc == 0 {
			break
		}
	}
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
