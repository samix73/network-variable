# NetworkVariable

A Go package for synchronizing variables across network connections using a peer-to-peer architecture.

## Overview

`network-variable` provides a simple and efficient way to synchronize variables between two peers over a network connection. When you update a variable on one peer, it automatically synchronizes to the other peer, and vice versa.

## Features

- 🔄 **Bidirectional synchronization** - Changes propagate in both directions
- 🎯 **Type-safe** - Uses Go generics for compile-time type safety
- 🔔 **Change listeners** - Subscribe to value changes with callbacks
- 🔒 **Thread-safe** - Safe for concurrent use from multiple goroutines
- ⚡ **Efficient** - Only syncs when values actually change
- 🎭 **Multiple types** - Supports any serializable Go type (primitives, structs, etc.)

## Installation

```bash
go get sandbox/syncedVariables/network_variable
```

## Quick Start

### Basic Example

```go
package main

import (
    "context"
    "fmt"
    "net"
    "time"
    
    nv "sandbox/syncedVariables/network_variable"
)

func main() {
    // Create a bidirectional network connection (e.g., TCP, pipe, etc.)
    conn1, conn2 := net.Pipe() // For testing; use net.Dial for real connections
    
    ctx := context.Background()
    
    // Create two peers
    peer1 := nv.NewPeer(conn1)
    peer2 := nv.NewPeer(conn2)
    
    // Start both peers
    go peer1.Start(ctx, nil)
    go peer2.Start(ctx, nil)
    
    // Create a synchronized variable on peer 1
    counter1, _ := nv.NewNetworkVariable(peer1, 1, 42)
    
    time.Sleep(100 * time.Millisecond) // Wait for sync
    
    // Create the same variable on peer 2 - it will receive the value from peer 1
    counter2, _ := nv.NewNetworkVariable[int](peer2, 1, 0)
    
    fmt.Println("Peer 2 value:", counter2.Get()) // Output: 42
    
    // Update on peer 2
    counter2.Set(100)
    
    time.Sleep(100 * time.Millisecond)
    
    fmt.Println("Peer 1 value:", counter1.Get()) // Output: 100
}
```

## Core Concepts

### Peer

A `Peer` manages network variables over a single bidirectional connection. Both sides of a connection have equal capabilities - there's no client/server distinction.

```go
peer := nv.NewPeer(conn)
go peer.Start(ctx, nil)
defer peer.Close()
```

### NetworkVariable

A `NetworkVariable` is a synchronized variable identified by a unique ID. All peers with the same variable ID will keep that variable synchronized.

```go
// Create a string variable with ID 1
myVar, err := nv.NewNetworkVariable(peer, 1, "initial value")
if err != nil {
    log.Fatal(err)
}

// Get current value
value := myVar.Get()

// Update value (syncs to other peers)
myVar.Set("new value")
```

### Variable IDs

Each `NetworkVariable` is identified by a `uint64` ID. Variables with the same ID on different peers will synchronize with each other.

**Important**: Only create variables with the same ID on peers that should synchronize that data.

## Examples

### Example 1: Synchronizing Game State

```go
type GameState struct {
    Score      int
    Level      int
    PlayerName string
}

// On server
serverPeer := nv.NewPeer(serverConn)
go serverPeer.Start(ctx, nil)

gameState, _ := nv.NewNetworkVariable(serverPeer, 100, GameState{
    Score:      0,
    Level:      1,
    PlayerName: "Player1",
})

// On client
clientPeer := nv.NewPeer(clientConn)
go clientPeer.Start(ctx, nil)

// Client receives the game state from server
clientGameState, _ := nv.NewNetworkVariable(clientPeer, 100, GameState{})

// Update on server - syncs to client
gameState.Set(GameState{
    Score:      100,
    Level:      2,
    PlayerName: "Player1",
})
```

### Example 2: Using Change Listeners

```go
counter, _ := nv.NewNetworkVariable(peer, 1, 0)

// Add a listener that fires when the value changes
listenerID := counter.AddListener(func(oldValue, newValue int) {
    fmt.Printf("Counter changed from %d to %d\n", oldValue, newValue)
})

// Update the value - listener will be called
counter.Set(10) // Output: Counter changed from 0 to 10

// Remove the listener when done
counter.RemoveListener(listenerID)

// Or clear all listeners
counter.ClearListeners()
```

### Example 3: Multiple Variables

```go
peer1 := nv.NewPeer(conn1)
go peer1.Start(ctx, nil)

// Create multiple variables with different types
playerName, _ := nv.NewNetworkVariable(peer1, 1, "Alice")
playerScore, _ := nv.NewNetworkVariable(peer1, 2, 0)
isOnline, _ := nv.NewNetworkVariable(peer1, 3, true)

peer2 := nv.NewPeer(conn2)
go peer2.Start(ctx, nil)

// Create matching variables on peer 2
name2, _ := nv.NewNetworkVariable[string](peer2, 1, "")
score2, _ := nv.NewNetworkVariable[int](peer2, 2, 0)
online2, _ := nv.NewNetworkVariable[bool](peer2, 3, false)

// All variables are now synchronized
```

### Example 4: Configuration Options

```go
// Create a peer with custom buffer size
peer := nv.NewPeer(conn, nv.WithBufferSize(8192))

// Multiple options
peer := nv.NewPeer(
    conn,
    nv.WithBufferSize(8192),
    // Add more options as they become available
)
```

### Example 5: Error Handling

```go
// Creating a variable with an existing ID and different type returns an error
var1, err := nv.NewNetworkVariable[string](peer, 1, "text")
if err != nil {
    log.Fatal(err)
}

var2, err := nv.NewNetworkVariable[int](peer, 1, 42)
if err != nil {
    // Error: variable with ID 1 already exists with a different type
    log.Println(err)
}

// Setting a value
if err := var1.Set("new value"); err != nil {
    log.Printf("Failed to set value: %v", err)
}
```

## Best Practices

1. **Use meaningful variable IDs**: Choose IDs that make sense for your application (e.g., `const PlayerNameID = 1`)

2. **Handle initialization order**: If peer A creates a variable before peer B, peer B will receive the value when it creates its matching variable

3. **Use contexts properly**: Pass `context.Context` to `Start()` for proper cleanup

4. **Remove listeners**: Always clean up listeners when they're no longer needed to prevent memory leaks

5. **Error handling**: Always check errors from `NewNetworkVariable()` and `Set()`

6. **Type consistency**: Ensure variables with the same ID use the same type across all peers

## API Reference

### Peer

```go
// Create a new peer
func NewPeer(conn net.Conn, opts ...Option) *Peer

// Start the peer's synchronization loop
func (p *Peer) Start(ctx context.Context, panicCallback func(r any)) error

// Close the peer and release resources
func (p *Peer) Close() error

// Delete a variable from the peer
func (p *Peer) DeleteVariable(index uint64)
```

### NetworkVariable

```go
// Create a new network variable
func NewNetworkVariable[T any](peer *Peer, id uint64, initialValue T) (*NetworkVariable[T], error)

// Get the current value
func (v *NetworkVariable[T]) Get() T

// Set a new value (syncs to other peers)
func (v *NetworkVariable[T]) Set(value T) error

// Add a change listener
func (v *NetworkVariable[T]) AddListener(listener func(oldValue, newValue T)) uint64

// Remove a specific listener
func (v *NetworkVariable[T]) RemoveListener(id uint64)

// Clear all listeners
func (v *NetworkVariable[T]) ClearListeners()
```

### Options

```go
// Set custom buffer size for network reads
func WithBufferSize(size int) Option
```

## Thread Safety

All operations are thread-safe. You can safely:
- Call `Get()` and `Set()` from multiple goroutines
- Add/remove listeners from multiple goroutines
- Create multiple variables concurrently

## Performance Considerations

- Variables only sync when values actually change (no sync for setting the same value)
- Network reads are blocking - each peer needs its own goroutine
- Listeners are called synchronously - avoid long-running operations in listeners
- Consider using a larger buffer size for large data structures

## Testing

The package includes comprehensive tests. Run them with:

```bash
cd /path/to/network_variable
go test -v
go test -race  # Check for race conditions
go test -cover # Check test coverage
```

## License

[Your License Here]

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.