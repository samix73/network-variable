package network_variable

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"maps"
	"slices"
	"sync"
)

// Listener is a function that gets called when the NetworkVariable's value changes.
type Listener[T comparable] func(oldValue, newVal T)

const separator = byte('|')

// command represents an operation to be performed on a NetworkVariable.
type command byte

const (
	commandSet command = iota + 1
	commandDel
)

func commands() []command {
	return []command{
		commandSet,
		commandDel,
	}
}

// getCommand converts a byte to a command if valid.
func getCommand(b byte) (command, bool) {
	switch command(b) {
	case commandSet, commandDel:
		return command(b), true
	default:
		return 0, false
	}
}

// networkVariable is an interface for network-synchronized variables.
type networkVariable interface {
	write(p []byte) error
	deallocate()
	isValid() bool
}

var _ networkVariable = (*NetworkVariable[any])(nil)

// NetworkVariable represents a variable that is synchronized across the network.
type NetworkVariable[T comparable] struct {
	valid bool
	id    uint64

	value *T
	mu    sync.RWMutex

	listenerID uint64
	listeners  map[uint64]Listener[T]
	listenerMu sync.RWMutex

	srv *Peer
}

// NewNetworkVariable creates a new NetworkVariable with the given ID and initial value.
// If a variable with the same ID already exists, it returns the existing variable.
// If there is an unowned variable with the same ID, it initializes the variable with that data.
func NewNetworkVariable[T comparable](srv *Peer, id uint64, initial T) (*NetworkVariable[T], error) {
	v, ok := srv.getVariable(id)
	if ok {
		existing, ok := v.(*NetworkVariable[T])
		if !ok {
			return nil, fmt.Errorf("NewNetworkVariable: variable with id %d has different type %T expected %T",
				id, v, &NetworkVariable[T]{})
		}

		existing.valid = true

		return existing, nil
	}

	b, ok := srv.getUnownedVariable(id)
	if ok {
		tv := &NetworkVariable[T]{
			valid: true,
			id:    id,
			value: nil,
			srv:   srv,
			mu:    sync.RWMutex{},
		}
		if err := tv.write(b); err != nil {
			return nil, fmt.Errorf("NewNetworkVariable: failed to write unowned variable: %w", err)
		}

		if ok := srv.registerVariable(id, tv); !ok {
			return nil, fmt.Errorf("NewNetworkVariable: failed to register variable with id %d", id)
		}

		return tv, nil
	}

	tv := &NetworkVariable[T]{
		valid: true,
		id:    id,
		value: &initial,
		srv:   srv,
		mu:    sync.RWMutex{},
	}
	if ok := srv.registerVariable(id, tv); !ok {
		return nil, fmt.Errorf("NewNetworkVariable: failed to register variable with id %d", id)
	}

	if err := tv.sync(commandSet); err != nil {
		return nil, fmt.Errorf("NewNetworkVariable: failed to sync initial value: %w", err)
	}

	return tv, nil
}

// sync sends the current value of the NetworkVariable to all connected clients
func (v *NetworkVariable[T]) sync(command command) error {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if !slices.Contains(commands(), command) {
		return fmt.Errorf("NetworkVariable.sync: invalid command %d", command)
	}

	buf, err := v.encodeMsg(command)
	if err != nil {
		return fmt.Errorf("NetworkVariable.sync: failed to encode message: %w", err)
	}

	if _, err := v.srv.conn.Write(buf); err != nil {
		return fmt.Errorf("NetworkVariable.sync: failed to write to connection: %w", err)
	}

	return nil
}

// encodeMsg encodes the NetworkVariable into a byte slice for transmission.
// The format is: [8 bytes ID][1 byte separator][1 byte command][1 byte separator][gob-encoded value]
// If the value is nil, only the ID and command are included.
func (v *NetworkVariable[T]) encodeMsg(command command) ([]byte, error) {
	buf := new(bytes.Buffer)

	if _, err := buf.Write(binary.LittleEndian.AppendUint64(nil, v.id)); err != nil {
		return nil, fmt.Errorf("NetworkVariable.encodeMsg: failed to write ID: %w", err)
	}

	buf.WriteByte(separator)
	buf.WriteByte(byte(command))

	if v.value != nil {
		buf.WriteByte(separator)
		if err := gob.NewEncoder(buf).Encode(v.value); err != nil {
			return nil, fmt.Errorf("NetworkVariable.encodeMsg: failed to encode value: %w", err)
		}
	}

	buf.WriteByte('\n')

	return buf.Bytes(), nil
}

// Get retrieves the current value of the NetworkVariable.
func (v *NetworkVariable[T]) Get() T {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.value == nil {
		var zero T
		return zero
	}

	return *v.value
}

// Set updates the value of the NetworkVariable and notifies listeners if the value has changed.
func (v *NetworkVariable[T]) Set(val T) error {
	v.mu.Lock()

	if !v.valid {
		v.mu.Unlock()

		return fmt.Errorf("NetworkVariable.Set: variable is not valid")
	}

	if v.value != nil && *v.value == val {
		v.mu.Unlock()

		return nil
	}

	var oldValue T
	if v.value != nil {
		oldValue = *v.value
	}
	v.value = &val

	v.mu.Unlock()

	v.notifyListeners(oldValue, val)

	if err := v.sync(commandSet); err != nil {
		return fmt.Errorf("NetworkVariable.Set: failed to sync value: %w", err)
	}

	return nil
}

// AddListener adds a listener function that will be called when the value changes.
// It returns a unique ID for the listener that can be used to remove it later.
func (v *NetworkVariable[T]) AddListener(listener Listener[T]) uint64 {
	v.listenerMu.Lock()
	defer v.listenerMu.Unlock()

	id := v.listenerID
	v.listenerID++
	if v.listeners == nil {
		v.listeners = make(map[uint64]Listener[T])
	}

	v.listeners[id] = listener

	return id
}

// RemoveListener removes a listener by its unique ID.
func (v *NetworkVariable[T]) RemoveListener(id uint64) {
	v.listenerMu.Lock()
	defer v.listenerMu.Unlock()

	delete(v.listeners, id)
}

// ClearListeners removes all registered listeners.
func (v *NetworkVariable[T]) ClearListeners() {
	v.listenerMu.Lock()
	defer v.listenerMu.Unlock()
	v.listeners = nil
}

// notifyListeners calls all registered listeners with the old and new values.
func (v *NetworkVariable[T]) notifyListeners(oldValue, newValue T) {
	v.listenerMu.RLock()
	listeners := slices.Collect(maps.Values(v.listeners))
	v.listenerMu.RUnlock()

	// Call listeners outside of lock to avoid deadlocks
	for _, listener := range listeners {
		listener(oldValue, newValue)
	}
}

// write processes an incoming message to update the NetworkVariable's value.
func (v *NetworkVariable[T]) write(p []byte) error {
	p = bytes.TrimSuffix(p, []byte{'\n'})
	parts := bytes.SplitN(p, []byte{separator}, 3)
	if len(parts) < 3 {
		return fmt.Errorf("NetworkVariable.write: invalid message format")
	}

	commandByte := parts[1]
	cmd, ok := getCommand(commandByte[0])
	if !ok {
		return fmt.Errorf("NetworkVariable.write: unknown command %d", commandByte[0])
	}

	switch cmd {
	case commandDel:
		v.deallocate()
	case commandSet:
		p = parts[2]

		var val T
		if err := gob.NewDecoder(bytes.NewBuffer(p)).Decode(&val); err != nil {
			return fmt.Errorf("NetworkVariable.write: failed to decode value: %w", err)
		}

		v.mu.Lock()
		var oldValue T
		if v.value != nil {
			oldValue = *v.value
		}
		v.value = &val
		v.mu.Unlock()

		v.notifyListeners(oldValue, val)
	default:
		return fmt.Errorf("NetworkVariable.write: unhandled command %d", cmd)
	}

	return nil
}

// deallocate removes the NetworkVariable from the peer and marks it as invalid.
func (v *NetworkVariable[T]) deallocate() {
	v.srv.DeleteVariable(v.id)

	v.mu.Lock()
	v.value = nil
	v.valid = false
	v.mu.Unlock()

	v.ClearListeners()
}

// isValid checks if the NetworkVariable is still valid.
func (v *NetworkVariable[T]) isValid() bool {
	v.mu.RLock()
	defer v.mu.RUnlock()

	return v.valid
}

// getIndex extracts the variable index from the beginning of the data slice.
func getIndex(data []byte) (uint64, bool) {
	if len(data) < 8 {
		return 0, false
	}

	return binary.LittleEndian.Uint64(data[:8]), true
}
