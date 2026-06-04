// Package store defines the persistence interface for VM definitions.
//
// Implementations live under sub-packages (jsonstore, mongostore, ...).
// The manager depends only on this interface so the backend can be swapped
// via configuration.
package store

import (
	"context"
	"errors"

	"github.com/anish-araz_cumulus/firecracker-manager-go/internal/vm"
)

// ErrNotFound is returned when a VM with the given ID does not exist.
var ErrNotFound = errors.New("vm not found")

// Store persists VM definitions.
//
// Implementations must be safe for concurrent use.
type Store interface {
	Get(ctx context.Context, id string) (*vm.VM, error)
	List(ctx context.Context) ([]*vm.VM, error)
	Put(ctx context.Context, v *vm.VM) error // upsert
	Delete(ctx context.Context, id string) error
	Close() error
}
