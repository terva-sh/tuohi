package updater

import "context"

type processWatch interface {
	Wait(context.Context) error
	Close() error
}
