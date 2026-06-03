// Package jsonstore is a file-backed implementation of store.Store.
//
// All VMs are held in memory and the entire set is serialized to a single
// JSON file on every mutation, atomically replaced via temp-file + rename.
// A POSIX advisory file lock (flock) guards against multiple manager
// processes pointing at the same path.
package jsonstore

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/anish-araz_cumulus/firecracker-manager-go/internal/store"
	"github.com/anish-araz_cumulus/firecracker-manager-go/internal/vm"
)

// Store is a JSON-file-backed store.Store.
type Store struct {
	path     string
	mu       sync.Mutex
	vms      map[string]*vm.VM
	lockFile *os.File
}

// Open opens (or creates) the JSON store at path.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("jsonstore: path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("jsonstore: mkdir: %w", err)
	}

	lockPath := path + ".lock"
	lf, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("jsonstore: open lockfile: %w", err)
	}
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lf.Close()
		return nil, fmt.Errorf("jsonstore: another process holds %s: %w", lockPath, err)
	}

	s := &Store{
		path:     path,
		vms:      make(map[string]*vm.VM),
		lockFile: lf,
	}

	if err := s.load(); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("jsonstore: read: %w", err)
	}
	if len(b) == 0 {
		return nil
	}
	var list []*vm.VM
	if err := json.Unmarshal(b, &list); err != nil {
		return fmt.Errorf("jsonstore: unmarshal: %w", err)
	}
	for _, v := range list {
		s.vms[v.ID] = v
	}
	return nil
}

// flush writes the in-memory map to disk atomically.
// Caller must hold s.mu.
func (s *Store) flush() error {
	list := make([]*vm.VM, 0, len(s.vms))
	for _, v := range s.vms {
		list = append(list, v)
	}
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return fmt.Errorf("jsonstore: marshal: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".vms-*.json")
	if err != nil {
		return fmt.Errorf("jsonstore: tempfile: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("jsonstore: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("jsonstore: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("jsonstore: close: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("jsonstore: rename: %w", err)
	}
	return nil
}

// Get returns the VM with the given ID.
func (s *Store) Get(_ context.Context, id string) (*vm.VM, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.vms[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return v.Clone(), nil
}

// List returns all VMs.
func (s *Store) List(_ context.Context) ([]*vm.VM, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*vm.VM, 0, len(s.vms))
	for _, v := range s.vms {
		out = append(out, v.Clone())
	}
	return out, nil
}

// Put upserts a VM.
func (s *Store) Put(_ context.Context, v *vm.VM) error {
	if v == nil || v.ID == "" {
		return fmt.Errorf("jsonstore: vm with empty id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vms[v.ID] = v.Clone()
	return s.flush()
}

// Delete removes a VM.
func (s *Store) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.vms[id]; !ok {
		return store.ErrNotFound
	}
	delete(s.vms, id)
	return s.flush()
}

// Close releases the file lock.
func (s *Store) Close() error {
	if s.lockFile != nil {
		_ = syscall.Flock(int(s.lockFile.Fd()), syscall.LOCK_UN)
		err := s.lockFile.Close()
		s.lockFile = nil
		return err
	}
	return nil
}
