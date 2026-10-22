package notify

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type submissionCenter struct {
	mu        sync.Mutex
	delivered string
	started   chan func(error)
}

func (c *submissionCenter) add(complete func(error)) { c.started <- complete }
func (c *submissionCenter) remove()                  { c.mu.Lock(); c.delivered = ""; c.mu.Unlock() }
func (c *submissionCenter) deliver(value string)     { c.mu.Lock(); c.delivered = value; c.mu.Unlock() }
func submissionResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(time.Second):
		t.Fatal("submission did not return")
		return nil
	}
}
func submissionCompletion(t *testing.T, c *submissionCenter) func(error) {
	t.Helper()
	select {
	case complete := <-c.started:
		return complete
	case <-time.After(time.Second):
		t.Fatal("native submission did not start")
		return nil
	}
}

// Exercise the same asynchronous wait/cleanup path as macBackend.post with a
// controlled native center: A returns cancelled, B succeeds, then A completes.
func TestLateCancelledSubmissionDoesNotRemoveReplacement(t *testing.T) {
	var submissions notificationSubmissions
	center := &submissionCenter{started: make(chan func(error), 1)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := make(chan error, 1)
	go func() { a <- submissions.run(ctx, "native-id", center.add, center.remove) }()
	completeA := submissionCompletion(t, center)
	center.deliver("A")
	cancel()
	if err := submissionResult(t, a); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	b := make(chan error, 1)
	go func() { b <- submissions.run(context.Background(), "native-id", center.add, center.remove) }()
	completeB := submissionCompletion(t, center)
	center.deliver("replacement B")
	completeB(nil)
	if err := submissionResult(t, b); err != nil {
		t.Fatal(err)
	}
	completeA(nil)
	center.mu.Lock()
	delivered := center.delivered
	center.mu.Unlock()
	if delivered != "replacement B" {
		t.Fatal("late A cleanup removed successful replacement B")
	}
	submissions.mu.Lock()
	remaining := len(submissions.byID)
	submissions.mu.Unlock()
	if remaining != 0 {
		t.Fatal("completed submissions retained native identities")
	}
}

func TestLatestCancelledSubmissionStillCleansUpAfterWaiterReturns(t *testing.T) {
	var submissions notificationSubmissions
	center := &submissionCenter{started: make(chan func(error), 1)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- submissions.run(ctx, "native-id", center.add, center.remove) }()
	complete := submissionCompletion(t, center)
	cancel()
	if err := submissionResult(t, result); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	center.deliver("accepted after cancellation")
	complete(nil)
	center.mu.Lock()
	delivered := center.delivered
	center.mu.Unlock()
	if delivered != "" {
		t.Fatal("cancelled latest submission escaped late cleanup")
	}
	submissions.mu.Lock()
	remaining := len(submissions.byID)
	submissions.mu.Unlock()
	if remaining != 0 {
		t.Fatal("completed cancelled submission leaked")
	}
}
