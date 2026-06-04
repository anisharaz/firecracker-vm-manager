// Package mongostore is a placeholder MongoDB implementation of store.Store.
//
// It exists so the wiring in cmd/manager/main.go can switch between backends
// today and be filled in later by adding the official Go driver
// (go.mongodb.org/mongo-driver) plus BSON marshalling for vm.VM. No driver
// dependency is pulled in until that work is done.
package mongostore

import (
	"context"
	"errors"

	"github.com/anish-araz_cumulus/firecracker-manager-go/internal/config"
	"github.com/anish-araz_cumulus/firecracker-manager-go/internal/store"
	"github.com/anish-araz_cumulus/firecracker-manager-go/internal/vm"
)

// ErrNotImplemented is returned by every method until the real impl lands.
var ErrNotImplemented = errors.New("mongostore: not implemented yet")

// Store is a stub MongoDB-backed store.Store.
type Store struct{}

// Open returns ErrNotImplemented. Wire-in point exists to validate the
// config switch in cmd/manager/main.go.
func Open(_ context.Context, _ config.MongoStoreConfig) (*Store, error) {
	return nil, ErrNotImplemented
}

func (s *Store) Get(context.Context, string) (*vm.VM, error) { return nil, ErrNotImplemented }
func (s *Store) List(context.Context) ([]*vm.VM, error)      { return nil, ErrNotImplemented }
func (s *Store) Put(context.Context, *vm.VM) error           { return ErrNotImplemented }
func (s *Store) Delete(context.Context, string) error        { return ErrNotImplemented }
func (s *Store) Close() error                                { return nil }

// compile-time interface check
var _ store.Store = (*Store)(nil)
