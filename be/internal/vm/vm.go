package vm

import (
	"sync"
	"time"
)

// State is the observed runtime state of a VM.
type State string

const (
	StateCreated  State = "created"
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopping State = "stopping"
	StateStopped  State = "stopped"
	StateFailed   State = "failed"
)

// DesiredState is the user-requested target state, used by reconcile-on-boot.
type DesiredState string

const (
	DesiredStopped DesiredState = "stopped"
	DesiredRunning DesiredState = "running"
)

// VM is the persistent + runtime model of a single microVM.
//
// JSON tags drive both the API DTO and the JSON store. Runtime-only fields
// are tagged with `json:"-"` so they never get persisted.
type VM struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	State        State        `json:"state"`
	DesiredState DesiredState `json:"desired_state"`

	VCPUs       int    `json:"vcpus"`
	MemMiB      int    `json:"mem_mib"`
	DiskSizeMiB int    `json:"disk_size_mib"`
	KernelPath  string `json:"kernel_path"`
	KernelArgs  string `json:"kernel_args"`
	RootfsPath  string `json:"rootfs_path"`

	TapName    string `json:"tap_name"`
	MacAddress string `json:"mac_address"`
	Bridge     string `json:"bridge"`

	ConsoleSock string `json:"console_sock"`

	CreatedAt time.Time `json:"created_at"`
	StartedAt time.Time `json:"started_at,omitempty"`

	// Runtime-only fields. Not persisted.
	PID     int         `json:"pid,omitempty"`
	PtsPath string      `json:"pts_path,omitempty"`
	Mu      *sync.Mutex `json:"-"`
	Runtime *Runtime    `json:"-"`
}

// Clone returns a shallow copy safe to hand out to callers (no mutex/runtime).
func (v *VM) Clone() *VM {
	cp := *v
	cp.Mu = nil
	cp.Runtime = nil
	return &cp
}

// EnsureMutex initializes the per-VM mutex if missing. Idempotent.
func (v *VM) EnsureMutex() {
	if v.Mu == nil {
		v.Mu = &sync.Mutex{}
	}
}
