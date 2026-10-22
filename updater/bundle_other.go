//go:build !darwin

package updater

import "context"

func bundleExecutable(context.Context, string, string) (string, error) { return "", ErrUnsupported }
func validateBundle(context.Context, string, string, string) (string, error) {
	return "", ErrUnsupported
}

func cloneBundle(context.Context, string, string) error { return ErrUnsupported }
