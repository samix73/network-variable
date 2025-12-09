package network_variable_test

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	network_variable "github.com/samix73/network-variable"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper function to close connections properly
func closeConn(t *testing.T, conn net.Conn) {
	if err := conn.Close(); err != nil {
		t.Logf("Error closing connection: %v", err)
	}
}

// TestNetworkVariable_BasicSync tests basic synchronization between two servers
func TestNetworkVariable_BasicSync(t *testing.T) {
	t.Parallel()

	conn1, conn2 := net.Pipe()
	defer closeConn(t, conn1)
	defer closeConn(t, conn2)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	// Start first server
	srv1 := network_variable.NewPeer(conn1)
	go func() {
		err := srv1.Start(ctx, nil)
		require.NoError(t, err)
	}()

	// Start second server
	srv2 := network_variable.NewPeer(conn2)
	go func() {
		err := srv2.Start(ctx, nil)
		require.NoError(t, err)
	}()

	// Give servers time to start
	time.Sleep(50 * time.Millisecond)

	// Create variable on server 1
	varID := uint64(100)
	var1, err := network_variable.NewNetworkVariable(srv1, varID, "initial")
	require.NoError(t, err)
	require.Equal(t, "initial", var1.Get())

	// Give time for sync
	time.Sleep(100 * time.Millisecond)

	// Create variable on server 2 with same ID - should receive initial value
	var2, err := network_variable.NewNetworkVariable(srv2, varID, "different")
	require.NoError(t, err)
	require.Equal(t, "initial", var2.Get(), "Peer 2 should receive initial value from server 1")

	// Update on server 1
	err = var1.Set("updated")
	require.NoError(t, err)

	// Wait for sync
	time.Sleep(100 * time.Millisecond)

	// Check value on server 2
	assert.Equal(t, "updated", var2.Get(), "Peer 2 should receive updated value")

	// Update on server 2
	err = var2.Set("changed_by_srv2")
	require.NoError(t, err)

	// Wait for sync
	time.Sleep(100 * time.Millisecond)

	// Check value on server 1
	assert.Equal(t, "changed_by_srv2", var1.Get(), "Peer 1 should receive value from server 2")
}

// TestNetworkVariable_MultipleVariables tests syncing multiple variables
func TestNetworkVariable_MultipleVariables(t *testing.T) {
	t.Parallel()

	conn1, conn2 := net.Pipe()
	defer closeConn(t, conn1)
	defer closeConn(t, conn2)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	srv1 := network_variable.NewPeer(conn1)
	go func() {
		err := srv1.Start(ctx, nil)
		require.NoError(t, err)
	}()

	srv2 := network_variable.NewPeer(conn2)
	go func() {
		err := srv2.Start(ctx, nil)
		require.NoError(t, err)
	}()

	time.Sleep(50 * time.Millisecond)

	// Create multiple variables on server 1
	strVar1, err := network_variable.NewNetworkVariable[string](srv1, 1, "hello")
	require.NoError(t, err)

	intVar1, err := network_variable.NewNetworkVariable[int](srv1, 2, 42)
	require.NoError(t, err)

	boolVar1, err := network_variable.NewNetworkVariable[bool](srv1, 3, true)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Create corresponding variables on server 2
	strVar2, err := network_variable.NewNetworkVariable[string](srv2, 1, "")
	require.NoError(t, err)

	intVar2, err := network_variable.NewNetworkVariable[int](srv2, 2, 0)
	require.NoError(t, err)

	boolVar2, err := network_variable.NewNetworkVariable[bool](srv2, 3, false)
	require.NoError(t, err)

	// Verify initial sync
	assert.Equal(t, "hello", strVar2.Get())
	assert.Equal(t, 42, intVar2.Get())
	assert.Equal(t, true, boolVar2.Get())

	// Update all variables
	err = strVar1.Set("world")
	require.NoError(t, err)
	err = intVar1.Set(100)
	require.NoError(t, err)
	err = boolVar1.Set(false)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Verify updates
	assert.Equal(t, "world", strVar2.Get())
	assert.Equal(t, 100, intVar2.Get())
	assert.Equal(t, false, boolVar2.Get())
}

// TestNetworkVariable_Listeners tests that listeners are notified correctly
func TestNetworkVariable_Listeners(t *testing.T) {
	t.Parallel()

	conn1, conn2 := net.Pipe()
	defer closeConn(t, conn1)
	defer closeConn(t, conn2)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	srv1 := network_variable.NewPeer(conn1)
	go func() {
		err := srv1.Start(ctx, nil)
		require.NoError(t, err)
	}()

	srv2 := network_variable.NewPeer(conn2)
	go func() {
		err := srv2.Start(ctx, nil)
		require.NoError(t, err)
	}()

	time.Sleep(50 * time.Millisecond)

	// Create variable on server 1
	var1, err := network_variable.NewNetworkVariable[int](srv1, 10, 10)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Create variable on server 2
	var2, err := network_variable.NewNetworkVariable[int](srv2, 10, 0)
	require.NoError(t, err)

	// Add listeners
	var callCount1 atomic.Int32
	var lastOld1, lastNew1 int
	var mu1 sync.Mutex

	id1 := var1.AddListener(func(oldVal, newVal int) {
		mu1.Lock()
		defer mu1.Unlock()
		callCount1.Add(1)
		lastOld1 = oldVal
		lastNew1 = newVal
	})
	defer var1.ClearListeners()

	var callCount2 atomic.Int32
	var lastOld2, lastNew2 int
	var mu2 sync.Mutex

	_ = var2.AddListener(func(oldVal, newVal int) {
		mu2.Lock()
		defer mu2.Unlock()
		callCount2.Add(1)
		lastOld2 = oldVal
		lastNew2 = newVal
	})
	defer var1.ClearListeners()

	// Update on server 1
	err = var1.Set(20)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Check listener was called on both servers
	mu1.Lock()
	assert.Equal(t, int32(1), callCount1.Load(), "Listener on server 1 should be called once")
	assert.Equal(t, 10, lastOld1)
	assert.Equal(t, 20, lastNew1)
	mu1.Unlock()

	mu2.Lock()
	assert.Equal(t, int32(1), callCount2.Load(), "Listener on server 2 should be called once")
	assert.Equal(t, 10, lastOld2)
	assert.Equal(t, 20, lastNew2)
	mu2.Unlock()

	// Remove listener from server 1
	var1.RemoveListener(id1)

	// Update again
	err = var2.Set(30)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Listener 1 should not be called again, listener 2 should be
	assert.Equal(t, int32(1), callCount1.Load(), "Removed listener should not be called")
	assert.Equal(t, int32(2), callCount2.Load(), "Active listener should be called")

	// Clear listeners
	var2.ClearListeners()

	err = var1.Set(40)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// No listeners should be called
	assert.Equal(t, int32(1), callCount1.Load())
	assert.Equal(t, int32(2), callCount2.Load(), "Cleared listeners should not be called")
}

// TestNetworkVariable_ConcurrentUpdates tests concurrent updates from multiple goroutines
func TestNetworkVariable_ConcurrentUpdates(t *testing.T) {
	t.Parallel()

	conn1, conn2 := net.Pipe()
	defer closeConn(t, conn1)
	defer closeConn(t, conn2)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	srv1 := network_variable.NewPeer(conn1)
	go func() {
		err := srv1.Start(ctx, nil)
		require.NoError(t, err)
	}()

	srv2 := network_variable.NewPeer(conn2)
	go func() {
		err := srv2.Start(ctx, nil)
		require.NoError(t, err)
	}()

	time.Sleep(50 * time.Millisecond)

	var1, err := network_variable.NewNetworkVariable[int](srv1, 20, 0)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	var2, err := network_variable.NewNetworkVariable[int](srv2, 20, 0)
	require.NoError(t, err)

	// Concurrent updates from one server only
	const goroutines = 5
	const updatesPerGoroutine = 3

	var wg sync.WaitGroup
	wg.Add(goroutines)

	// Update from server 1
	for i := 0; i < goroutines; i++ {
		go func(start int) {
			defer wg.Done()
			for j := 0; j < updatesPerGoroutine; j++ {
				_ = var1.Set(start*updatesPerGoroutine + j + 1)
				time.Sleep(10 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()
	time.Sleep(300 * time.Millisecond)

	// Both variables should have the same final value
	val1 := var1.Get()
	val2 := var2.Get()
	assert.Equal(t, val1, val2, "Both servers should have the same final value")
	assert.Greater(t, val1, 0, "Variable should have been updated")
}

// TestNetworkVariable_UnownedVariable tests the unowned variable mechanism
func TestNetworkVariable_UnownedVariable(t *testing.T) {
	t.Parallel()

	conn1, conn2 := net.Pipe()
	defer closeConn(t, conn1)
	defer closeConn(t, conn2)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	srv1 := network_variable.NewPeer(conn1)
	go func() {
		err := srv1.Start(ctx, nil)
		require.NoError(t, err)
	}()

	srv2 := network_variable.NewPeer(conn2)
	go func() {
		err := srv2.Start(ctx, nil)
		require.NoError(t, err)
	}()

	time.Sleep(50 * time.Millisecond)

	// Create variable on server 1
	varID := uint64(30)
	var1, err := network_variable.NewNetworkVariable[string](srv1, varID, "from_srv1")
	require.NoError(t, err)

	// Wait for data to be sent and received by srv2
	time.Sleep(150 * time.Millisecond)

	// Now create variable on server 2 - should receive the unowned data
	var2, err := network_variable.NewNetworkVariable[string](srv2, varID, "from_srv2")
	require.NoError(t, err)

	// Should have value from server 1
	assert.Equal(t, "from_srv1", var2.Get(), "Peer 2 should receive unowned variable data")
	assert.Equal(t, "from_srv1", var1.Get())
}

// TestNetworkVariable_SameValueSet tests that setting the same value doesn't trigger sync
func TestNetworkVariable_SameValueSet(t *testing.T) {
	t.Parallel()

	conn1, conn2 := net.Pipe()
	defer closeConn(t, conn1)
	defer closeConn(t, conn2)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	srv1 := network_variable.NewPeer(conn1)
	go func() {
		err := srv1.Start(ctx, nil)
		require.NoError(t, err)
	}()

	srv2 := network_variable.NewPeer(conn2)
	go func() {
		err := srv2.Start(ctx, nil)
		require.NoError(t, err)
	}()

	time.Sleep(50 * time.Millisecond)

	var1, err := network_variable.NewNetworkVariable[int](srv1, 50, 100)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	var2, err := network_variable.NewNetworkVariable[int](srv2, 50, 0)
	require.NoError(t, err)

	// Add listener to track calls
	var callCount atomic.Int32
	var2.AddListener(func(oldVal, newVal int) {
		callCount.Add(1)
	})

	// Set same value
	err = var1.Set(100)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Listener should not be called since value didn't change
	assert.Equal(t, int32(0), callCount.Load(), "Listener should not be called for same value")
}

// TestNetworkVariable_LargeBufferSize tests with custom buffer size
func TestNetworkVariable_LargeBufferSize(t *testing.T) {
	t.Parallel()

	conn1, conn2 := net.Pipe()
	defer closeConn(t, conn1)
	defer closeConn(t, conn2)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	// Use larger buffer size
	srv1 := network_variable.NewPeer(conn1, network_variable.WithBufferSize(8192))
	go func() {
		_ = srv1.Start(ctx, nil)
	}()

	srv2 := network_variable.NewPeer(conn2, network_variable.WithBufferSize(8192))
	go func() {
		_ = srv2.Start(ctx, nil)
	}()

	time.Sleep(50 * time.Millisecond)

	// Create a large string
	largeString := string(make([]byte, 4096))
	for i := range largeString {
		largeString = largeString[:i] + "a" + largeString[i+1:]
	}

	_, err := network_variable.NewNetworkVariable[string](srv1, 60, largeString)
	require.NoError(t, err)

	time.Sleep(200 * time.Millisecond)

	var2, err := network_variable.NewNetworkVariable[string](srv2, 60, "")
	require.NoError(t, err)

	assert.Equal(t, len(largeString), len(var2.Get()), "Large string should sync correctly")
}

// TestNetworkVariable_MultipleListeners tests multiple listeners on the same variable
func TestNetworkVariable_MultipleListeners(t *testing.T) {
	t.Parallel()

	conn1, conn2 := net.Pipe()
	defer closeConn(t, conn1)
	defer closeConn(t, conn2)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	srv1 := network_variable.NewPeer(conn1)
	go func() {
		_ = srv1.Start(ctx, nil)
	}()

	srv2 := network_variable.NewPeer(conn2)
	go func() {
		_ = srv2.Start(ctx, nil)
	}()

	time.Sleep(50 * time.Millisecond)

	var1, err := network_variable.NewNetworkVariable[int](srv1, 70, 0)
	require.NoError(t, err)

	// Add multiple listeners
	var count1, count2, count3 atomic.Int32

	var1.AddListener(func(oldVal, newVal int) {
		count1.Add(1)
	})

	var1.AddListener(func(oldVal, newVal int) {
		count2.Add(1)
	})

	var1.AddListener(func(oldVal, newVal int) {
		count3.Add(1)
	})

	// Update value
	err = var1.Set(42)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// All listeners should be called
	assert.Equal(t, int32(1), count1.Load())
	assert.Equal(t, int32(1), count2.Load())
	assert.Equal(t, int32(1), count3.Load())
}

// TestNetworkVariable_RapidUpdates tests rapid successive updates
func TestNetworkVariable_RapidUpdates(t *testing.T) {
	t.Parallel()

	conn1, conn2 := net.Pipe()
	defer closeConn(t, conn1)
	defer closeConn(t, conn2)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	srv1 := network_variable.NewPeer(conn1)
	go func() {
		_ = srv1.Start(ctx, nil)
	}()

	srv2 := network_variable.NewPeer(conn2)
	go func() {
		_ = srv2.Start(ctx, nil)
	}()

	time.Sleep(50 * time.Millisecond)

	var1, err := network_variable.NewNetworkVariable[int](srv1, 90, 0)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	var2, err := network_variable.NewNetworkVariable[int](srv2, 90, 0)
	require.NoError(t, err)

	// Rapid updates
	for i := 1; i <= 20; i++ {
		err = var1.Set(i)
		require.NoError(t, err)
	}

	// Wait for all updates to propagate
	time.Sleep(500 * time.Millisecond)

	// Both should eventually converge to the same value
	assert.Equal(t, var1.Get(), var2.Get())
	assert.Greater(t, var2.Get(), 0, "Variable should have been updated")
}

// TestNetworkVariable_StructType tests with a custom struct type
func TestNetworkVariable_StructType(t *testing.T) {
	t.Parallel()

	type Person struct {
		Name string
		Age  int
	}

	conn1, conn2 := net.Pipe()
	defer closeConn(t, conn1)
	defer closeConn(t, conn2)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	srv1 := network_variable.NewPeer(conn1)
	go func() {
		_ = srv1.Start(ctx, nil)
	}()

	srv2 := network_variable.NewPeer(conn2)
	go func() {
		_ = srv2.Start(ctx, nil)
	}()

	time.Sleep(50 * time.Millisecond)

	initialPerson := Person{Name: "Alice", Age: 30}
	var1, err := network_variable.NewNetworkVariable[Person](srv1, 95, initialPerson)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	var2, err := network_variable.NewNetworkVariable[Person](srv2, 95, Person{})
	require.NoError(t, err)

	// Verify sync
	got := var2.Get()
	assert.Equal(t, "Alice", got.Name)
	assert.Equal(t, 30, got.Age)

	// Update
	updatedPerson := Person{Name: "Bob", Age: 25}
	err = var1.Set(updatedPerson)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	got = var2.Get()
	assert.Equal(t, "Bob", got.Name)
	assert.Equal(t, 25, got.Age)
}

// TestNetworkVariable_ListenerOldValue tests that listeners receive correct old values
func TestNetworkVariable_ListenerOldValue(t *testing.T) {
	t.Parallel()

	conn1, conn2 := net.Pipe()
	defer closeConn(t, conn1)
	defer closeConn(t, conn2)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	srv1 := network_variable.NewPeer(conn1)
	go func() {
		_ = srv1.Start(ctx, nil)
	}()

	srv2 := network_variable.NewPeer(conn2)
	go func() {
		_ = srv2.Start(ctx, nil)
	}()

	time.Sleep(50 * time.Millisecond)

	var1, err := network_variable.NewNetworkVariable[int](srv1, 96, 100)
	require.NoError(t, err)

	var receivedOld, receivedNew []int
	var mu sync.Mutex

	var1.AddListener(func(oldVal, newVal int) {
		mu.Lock()
		defer mu.Unlock()
		receivedOld = append(receivedOld, oldVal)
		receivedNew = append(receivedNew, newVal)
	})

	// Make several updates
	values := []int{200, 300, 400, 500}
	for _, val := range values {
		err = var1.Set(val)
		require.NoError(t, err)
		time.Sleep(50 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()

	require.Len(t, receivedOld, 4)
	require.Len(t, receivedNew, 4)

	// Check old values
	assert.Equal(t, 100, receivedOld[0])
	assert.Equal(t, 200, receivedOld[1])
	assert.Equal(t, 300, receivedOld[2])
	assert.Equal(t, 400, receivedOld[3])

	// Check new values
	assert.Equal(t, 200, receivedNew[0])
	assert.Equal(t, 300, receivedNew[1])
	assert.Equal(t, 400, receivedNew[2])
	assert.Equal(t, 500, receivedNew[3])
}

// TestNetworkVariable_DuplicateIDSameType tests creating a variable with an existing ID and same type
func TestNetworkVariable_DuplicateIDSameType(t *testing.T) {
	t.Parallel()

	conn1, conn2 := net.Pipe()
	defer closeConn(t, conn1)
	defer closeConn(t, conn2)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	srv1 := network_variable.NewPeer(conn1)
	go func() {
		err := srv1.Start(ctx, nil)
		require.NoError(t, err)
	}()

	srv2 := network_variable.NewPeer(conn2)
	go func() {
		err := srv2.Start(ctx, nil)
		require.NoError(t, err)
	}()

	time.Sleep(50 * time.Millisecond)

	// Create first variable with ID 100
	varID := uint64(100)
	var1, err := network_variable.NewNetworkVariable[string](srv1, varID, "first")
	require.NoError(t, err)
	assert.Equal(t, "first", var1.Get())

	// Try to create another variable with same ID and same type
	var2, err := network_variable.NewNetworkVariable[string](srv1, varID, "second")
	require.NoError(t, err)

	// Should return the existing variable, not create a new one
	assert.Same(t, var1, var2, "Should return the same variable instance")
	assert.Equal(t, "first", var2.Get(), "Should keep the original value")

	// Update through var2 should affect var1 (they're the same instance)
	err = var2.Set("updated")
	require.NoError(t, err)
	assert.Equal(t, "updated", var1.Get())
}

// TestNetworkVariable_DuplicateIDDifferentType tests creating a variable with an existing ID but different type
func TestNetworkVariable_DuplicateIDDifferentType(t *testing.T) {
	t.Parallel()

	conn1, conn2 := net.Pipe()
	defer closeConn(t, conn1)
	defer closeConn(t, conn2)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	srv1 := network_variable.NewPeer(conn1)
	go func() {
		err := srv1.Start(ctx, nil)
		require.NoError(t, err)

	}()

	srv2 := network_variable.NewPeer(conn2)
	go func() {
		err := srv2.Start(ctx, nil)
		require.NoError(t, err)
	}()

	time.Sleep(50 * time.Millisecond)

	// Create first variable with ID 101 as string
	varID := uint64(101)
	var1, err := network_variable.NewNetworkVariable[string](srv1, varID, "text")
	require.NoError(t, err)
	assert.Equal(t, "text", var1.Get())

	// Try to create another variable with same ID but different type (int)
	var2, err := network_variable.NewNetworkVariable[int](srv1, varID, 42)

	// Should return an error because types don't match
	assert.Error(t, err)
	assert.Nil(t, var2)
	assert.Contains(t, err.Error(), "different type", "Error should mention type mismatch")

	// Original variable should still work
	assert.Equal(t, "text", var1.Get())
	err = var1.Set("updated")
	require.NoError(t, err)
	assert.Equal(t, "updated", var1.Get())
}
