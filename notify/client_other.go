//go:build !linux && !freebsd && !netbsd && !darwin

package notify

import "context"

// A Windows balloon belongs to one shared notification-area icon; it cannot
// provide reliable independent correlation and dismissal. Never silently
// substitute that read-only helper for a managed notification client.
func openManagedBackend(context.Context, Config, func(managedEvent)) (managedBackend, error) {
	return nil, ErrUnsupported
}
