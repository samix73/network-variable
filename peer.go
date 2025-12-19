package network_variable

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
)

// Peer represents a peer that manages network variables over a network connection.
type Peer struct {
	options options

	closed    atomic.Bool
	closeChan chan struct{}

	conn net.Conn

	variables        map[uint64]networkVariable
	unownedVariables map[uint64][]byte

	mu sync.RWMutex
}

// NewPeer creates a new Peer instance with the provided network connection and options.
func NewPeer(conn net.Conn, opts ...Option) *Peer {
	o := defaultOptions()
	for _, opt := range opts {
		opt(&o)
	}

	return &Peer{
		options:          o,
		conn:             conn,
		variables:        make(map[uint64]networkVariable),
		unownedVariables: make(map[uint64][]byte),
		closeChan:        make(chan struct{}),
	}
}

// Start begins the peer's main loop, reading data from the network connection
func (s *Peer) Start(ctx context.Context, panicCallback func(r any)) error {
	defer func() {
		if r := recover(); r != nil && panicCallback != nil {
			panicCallback(r)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			if err := s.Close(); err != nil {
				return fmt.Errorf("Peer.Start: failed to close peer: %w", err)
			}

			if err := ctx.Err(); err != nil && !errors.Is(err, context.Canceled) {
				return fmt.Errorf("Peer.Start: context done with error: %w", err)
			}

			return nil
		case <-s.closeChan:
			return nil
		default:
		}

		buf := make([]byte, s.options.bufferSize)
		n, err := s.conn.Read(buf)
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				return nil
			}

			return err
		}

		received := buf[:n]
		index, ok := getIndex(received)
		if !ok {
			continue
		}

		s.mu.RLock()
		variable, ok := s.variables[index]
		s.mu.RUnlock()
		if !ok {
			s.mu.Lock()
			s.unownedVariables[index] = received
			s.mu.Unlock()

			continue
		}

		if err := variable.write(received); err != nil {
			continue
		}
	}
}

// DeleteVariable removes a network variable from the peer by its index.
func (s *Peer) DeleteVariable(index uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.variables, index)
}

// registerVariable adds a network variable to the peer's registry.
func (s *Peer) registerVariable(index uint64, variable networkVariable) bool {
	if !variable.isValid() {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.variables[index] = variable

	return true
}

// getVariable retrieves a network variable by its index.
func (s *Peer) getVariable(id uint64) (networkVariable, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	variable, ok := s.variables[id]
	return variable, ok
}

// getUnownedVariable retrieves and removes an unowned variable's data by its index.
func (s *Peer) getUnownedVariable(id uint64) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	b, ok := s.unownedVariables[id]
	if !ok {
		return nil, false
	}

	delete(s.unownedVariables, id)

	return b, ok
}

func (s *Peer) write(data []byte) error {
	_, err := s.conn.Write(data)
	if err != nil {
		return fmt.Errorf("Peer.write: failed to write data: %w", err)
	}

	return nil
}

// Close shuts down the peer and releases all associated resources.
func (s *Peer) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}

	close(s.closeChan)

	for _, v := range s.variables {
		v.deallocate()
	}

	if err := s.conn.Close(); err != nil {
		return fmt.Errorf("Peer.Close: failed to close connection: %w", err)
	}

	return nil
}
