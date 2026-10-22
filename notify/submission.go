package notify

import (
	"context"
	"sync"
)

// run starts a native asynchronous submission and bounds the caller's wait.
// start must invoke complete exactly once, even if ctx is cancelled first.
func (s *notificationSubmissions) run(ctx context.Context, id string, start func(complete func(error)), remove func()) error {
	n := s.begin(id)
	defer n.release()
	result := make(chan error, 1)
	start(func(err error) {
		defer n.release()
		if ctx.Err() != nil {
			n.cleanup(remove)
		}
		result <- err
	})
	select {
	case <-ctx.Done():
		n.cleanup(remove)
		return ctx.Err()
	case err := <-result:
		return err
	}
}

// notificationSubmissions orders best-effort cleanup for asynchronous native
// submissions that reuse an identifier on replacement. A late completion may
// clean up only while it is still the latest submission for that identifier.
// The lock covers the cleanup call and admission of a newer submission, so a
// check followed by removal cannot race with a newer native add.
type notificationSubmissions struct {
	mu   sync.Mutex
	byID map[string]*notificationSubmissionGroup
}

type notificationSubmissionGroup struct {
	latest *notificationSubmission
	refs   int
}

type notificationSubmission struct {
	owner *notificationSubmissions
	group *notificationSubmissionGroup
	id    string
}

func (s *notificationSubmissions) begin(id string) *notificationSubmission {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byID == nil {
		s.byID = make(map[string]*notificationSubmissionGroup)
	}
	g := s.byID[id]
	if g == nil {
		g = &notificationSubmissionGroup{}
		s.byID[id] = g
	}
	n := &notificationSubmission{owner: s, group: g, id: id}
	g.latest = n
	g.refs += 2 // the waiting caller and the native completion each release once
	return n
}

func (n *notificationSubmission) cleanup(remove func()) {
	n.owner.mu.Lock()
	defer n.owner.mu.Unlock()
	if n.group.latest == n {
		remove()
	}
}

func (n *notificationSubmission) release() {
	n.owner.mu.Lock()
	defer n.owner.mu.Unlock()
	n.group.refs--
	if n.group.refs == 0 {
		delete(n.owner.byID, n.id)
	}
}
