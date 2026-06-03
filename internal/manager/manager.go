// Package manager owns the in-memory registry of VMs and orchestrates
// lifecycle transitions, persisting through a store.Store.
package manager

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/anish-araz_cumulus/firecracker-manager-go/internal/config"
	"github.com/anish-araz_cumulus/firecracker-manager-go/internal/network"
	"github.com/anish-araz_cumulus/firecracker-manager-go/internal/rootfs"
	"github.com/anish-araz_cumulus/firecracker-manager-go/internal/store"
	"github.com/anish-araz_cumulus/firecracker-manager-go/internal/vm"

	"github.com/oklog/ulid/v2"
)

// ErrNotFound is re-exported for handler convenience.
var ErrNotFound = store.ErrNotFound

const stopTimeout = 15 * time.Second

// Manager is the central object that handlers talk to.
type Manager struct {
	cfg   config.Config
	store store.Store

	mu  sync.RWMutex
	vms map[string]*vm.VM
}

// New constructs a Manager and hydrates it from the store.
func New(cfg config.Config, s store.Store) (*Manager, error) {
	m := &Manager{cfg: cfg, store: s, vms: make(map[string]*vm.VM)}
	list, err := s.List(context.Background())
	if err != nil {
		return nil, fmt.Errorf("manager: hydrate: %w", err)
	}
	for _, v := range list {
		v.EnsureMutex()
		m.vms[v.ID] = v
	}
	return m, nil
}

// CreateRequest is the input to Create.
type CreateRequest struct {
	Name        string
	VCPUs       int
	MemMiB      int
	DiskSizeMiB int
	KernelArgs  string
}

// Create defines a new VM, provisions its rootfs, persists it, and returns it.
// The VM is in state "created" / desired "stopped".
func (m *Manager) Create(ctx context.Context, req CreateRequest) (*vm.VM, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if req.VCPUs <= 0 {
		req.VCPUs = m.cfg.DefaultVCPUs
	}
	if req.MemMiB <= 0 {
		req.MemMiB = m.cfg.DefaultMemMiB
	}
	if req.DiskSizeMiB <= 0 {
		req.DiskSizeMiB = m.cfg.DefaultDiskSizeMiB
	}
	if req.KernelArgs == "" {
		req.KernelArgs = m.cfg.DefaultKernelArgs
	}

	id := ulid.Make().String()
	dir := vm.VMDir(m.cfg.StateDir, id)
	rootfsPath := filepath.Join(dir, "rootfs.img")

	if err := ensureDir(dir); err != nil {
		return nil, err
	}

	if err := rootfs.Provision(m.cfg.RootfsTemplate, rootfsPath, int64(req.DiskSizeMiB)); err != nil {
		_ = rootfs.Cleanup(dir)
		return nil, err
	}

	v := &vm.VM{
		ID:           id,
		Name:         req.Name,
		State:        vm.StateCreated,
		DesiredState: vm.DesiredStopped,
		VCPUs:        req.VCPUs,
		MemMiB:       req.MemMiB,
		DiskSizeMiB:  req.DiskSizeMiB,
		KernelPath:   m.cfg.KernelPath,
		KernelArgs:   req.KernelArgs,
		RootfsPath:   rootfsPath,
		TapName:      network.DeriveTapName(m.cfg.TapPrefix, id),
		MacAddress:   network.DeriveMAC(m.cfg.MacPrefix, id),
		Bridge:       m.cfg.BridgeName,
		ConsoleSock:  filepath.Join(dir, "console.sock"),
		CreatedAt:    time.Now(),
	}
	v.EnsureMutex()

	m.mu.Lock()
	m.vms[v.ID] = v
	m.mu.Unlock()

	if err := m.store.Put(ctx, v); err != nil {
		m.mu.Lock()
		delete(m.vms, v.ID)
		m.mu.Unlock()
		_ = rootfs.Cleanup(dir)
		return nil, err
	}
	return v.Clone(), nil
}

// Get returns a snapshot of one VM.
func (m *Manager) Get(_ context.Context, id string) (*vm.VM, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.vms[id]
	if !ok {
		return nil, ErrNotFound
	}
	return v.Clone(), nil
}

// List returns snapshots of all VMs.
func (m *Manager) List(_ context.Context) []*vm.VM {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*vm.VM, 0, len(m.vms))
	for _, v := range m.vms {
		out = append(out, v.Clone())
	}
	return out
}

// Start launches the VM.
func (m *Manager) Start(ctx context.Context, id string) (*vm.VM, error) {
	v, err := m.lookup(id)
	if err != nil {
		return nil, err
	}
	v.Mu.Lock()
	defer v.Mu.Unlock()
	if v.Runtime != nil {
		return v.Clone(), nil
	}
	v.State = vm.StateStarting
	if err := vm.Start(ctx, v, m.cfg.StateDir, m.cfg.BridgeName); err != nil {
		v.State = vm.StateFailed
		_ = m.store.Put(ctx, v)
		return nil, err
	}
	v.DesiredState = vm.DesiredRunning
	if err := m.store.Put(ctx, v); err != nil {
		return nil, err
	}
	return v.Clone(), nil
}

// Stop gracefully shuts the VM down.
func (m *Manager) Stop(ctx context.Context, id string) (*vm.VM, error) {
	v, err := m.lookup(id)
	if err != nil {
		return nil, err
	}
	v.Mu.Lock()
	defer v.Mu.Unlock()
	if err := vm.Stop(ctx, v, stopTimeout); err != nil {
		return nil, err
	}
	v.DesiredState = vm.DesiredStopped
	if err := m.store.Put(ctx, v); err != nil {
		return nil, err
	}
	return v.Clone(), nil
}

// Kill force-stops the VM.
func (m *Manager) Kill(ctx context.Context, id string) (*vm.VM, error) {
	v, err := m.lookup(id)
	if err != nil {
		return nil, err
	}
	v.Mu.Lock()
	defer v.Mu.Unlock()
	if err := vm.Kill(ctx, v); err != nil {
		return nil, err
	}
	v.DesiredState = vm.DesiredStopped
	if err := m.store.Put(ctx, v); err != nil {
		return nil, err
	}
	return v.Clone(), nil
}

// Restart cycles a VM.
func (m *Manager) Restart(ctx context.Context, id string) (*vm.VM, error) {
	v, err := m.lookup(id)
	if err != nil {
		return nil, err
	}
	v.Mu.Lock()
	defer v.Mu.Unlock()
	if v.Runtime != nil {
		if err := vm.Stop(ctx, v, stopTimeout); err != nil {
			return nil, err
		}
	}
	if err := vm.Start(ctx, v, m.cfg.StateDir, m.cfg.BridgeName); err != nil {
		v.State = vm.StateFailed
		_ = m.store.Put(ctx, v)
		return nil, err
	}
	v.DesiredState = vm.DesiredRunning
	if err := m.store.Put(ctx, v); err != nil {
		return nil, err
	}
	return v.Clone(), nil
}

// Delete force-stops and removes the VM definition + rootfs.
func (m *Manager) Delete(ctx context.Context, id string) error {
	v, err := m.lookup(id)
	if err != nil {
		return err
	}
	v.Mu.Lock()
	if v.Runtime != nil {
		_ = vm.Kill(ctx, v)
	}
	dir := vm.VMDir(m.cfg.StateDir, id)
	v.Mu.Unlock()

	m.mu.Lock()
	delete(m.vms, id)
	m.mu.Unlock()

	if err := m.store.Delete(ctx, id); err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	return rootfs.Cleanup(dir)
}

// SubscribeConsole returns the broker for the running VM, or an error.
func (m *Manager) SubscribeConsole(id string) (*vm.Broker, error) {
	v, err := m.lookup(id)
	if err != nil {
		return nil, err
	}
	v.Mu.Lock()
	defer v.Mu.Unlock()
	return vm.SubscribeConsole(v)
}

// Reconcile re-launches every VM whose DesiredState is "running" but is not
// currently running. Called once on manager startup.
func (m *Manager) Reconcile(ctx context.Context) []error {
	var errs []error
	m.mu.RLock()
	ids := make([]string, 0, len(m.vms))
	for id, v := range m.vms {
		if v.DesiredState == vm.DesiredRunning {
			ids = append(ids, id)
		}
	}
	m.mu.RUnlock()
	for _, id := range ids {
		if _, err := m.Start(ctx, id); err != nil {
			errs = append(errs, fmt.Errorf("reconcile %s: %w", id, err))
		}
	}
	return errs
}

// ShutdownAll force-stops every running VM. Called on manager exit.
func (m *Manager) ShutdownAll(ctx context.Context) {
	m.mu.RLock()
	ids := make([]string, 0, len(m.vms))
	for id := range m.vms {
		ids = append(ids, id)
	}
	m.mu.RUnlock()
	for _, id := range ids {
		v, err := m.lookup(id)
		if err != nil {
			continue
		}
		v.Mu.Lock()
		if v.Runtime != nil {
			_ = vm.Stop(ctx, v, stopTimeout)
			_ = m.store.Put(ctx, v)
		}
		v.Mu.Unlock()
	}
}

// Config returns the active config (read-only use).
func (m *Manager) Config() config.Config { return m.cfg }

func (m *Manager) lookup(id string) (*vm.VM, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.vms[id]
	if !ok {
		return nil, ErrNotFound
	}
	return v, nil
}

func ensureDir(p string) error {
	return osMkdirAll(p)
}
