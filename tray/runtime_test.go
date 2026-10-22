package tray

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestQueuedUpdateCancellationAndRemoval(t *testing.T) {
	var q updateQueue
	a := newAvailability(nil)
	defer a.close()
	ctx, cancel := context.WithCancel(context.Background())
	id, r := q.add(ctx, Changes{}, a)
	cancel()
	if err := q.wait(id, r); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if q.take(id) != nil {
		t.Fatal("cancelled patch could still be applied")
	}
	id, r = q.add(context.Background(), Changes{}, a)
	a.close()
	q.close(a)
	if err := q.wait(id, r); !errors.Is(err, ErrNotRunning) {
		t.Fatal(err)
	}
	b := newAvailability(nil)
	defer b.close()
	newID, newRequest := q.add(context.Background(), Changes{}, b)
	if newID == id || q.take(id) != nil {
		t.Fatal("old native token could target a replacement tray")
	}
	q.take(newID)
	newRequest.result <- nil
	if err := q.wait(newID, newRequest); err != nil {
		t.Fatal(err)
	}
	// A late enqueue after removal must fail without waiting for a stopped UI loop.
	id, r = q.add(context.Background(), Changes{}, a)
	if err := q.wait(id, r); !errors.Is(err, ErrNotRunning) {
		t.Fatal(err)
	}
}

func TestAvailabilityRemovalDropsQueuedDelivery(t *testing.T) {
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	a := newAvailability(func(v Availability) {
		if v.Registered {
			t.Error("queued availability ran after removal")
			return
		}
		close(entered)
		<-release
		close(finished)
	})
	a.publish(Availability{Running: true})
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("missing initial availability")
	}
	a.publish(Availability{Running: true, Registered: true})
	a.close()
	b := newAvailability(nil)
	defer b.close()
	a.close() // a stale teardown must not clear a later lifetime
	if CurrentAvailability().Generation != b.state.Generation {
		t.Fatal("old lifetime cleared current availability")
	}
	once.Do(func() { close(release) })
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("admitted callback did not finish")
	}
}
