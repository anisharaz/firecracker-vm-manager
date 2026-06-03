package api

import (
	"time"

	"github.com/anish-araz_cumulus/firecracker-manager-go/internal/vm"
)

// CreateVMRequest is the body of POST /vms.
type CreateVMRequest struct {
	Name        string `json:"name"`
	VCPUs       int    `json:"vcpus,omitempty"`
	MemMiB      int    `json:"mem_mib,omitempty"`
	DiskSizeMiB int    `json:"disk_size_mib,omitempty"`
	KernelArgs  string `json:"kernel_args,omitempty"`
}

// VMResponse is the wire shape returned by handlers.
type VMResponse struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	State        string    `json:"state"`
	DesiredState string    `json:"desired_state"`
	VCPUs        int       `json:"vcpus"`
	MemMiB       int       `json:"mem_mib"`
	DiskSizeMiB  int       `json:"disk_size_mib"`
	KernelPath   string    `json:"kernel_path"`
	KernelArgs   string    `json:"kernel_args"`
	RootfsPath   string    `json:"rootfs_path"`
	TapName      string    `json:"tap_name"`
	MacAddress   string    `json:"mac_address"`
	Bridge       string    `json:"bridge"`
	ConsoleSock  string    `json:"console_sock"`
	PtsPath      string    `json:"pts_path,omitempty"`
	PID          int       `json:"pid,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	StartedAt    time.Time `json:"started_at,omitempty"`
}

func toResponse(v *vm.VM) VMResponse {
	return VMResponse{
		ID:           v.ID,
		Name:         v.Name,
		State:        string(v.State),
		DesiredState: string(v.DesiredState),
		VCPUs:        v.VCPUs,
		MemMiB:       v.MemMiB,
		DiskSizeMiB:  v.DiskSizeMiB,
		KernelPath:   v.KernelPath,
		KernelArgs:   v.KernelArgs,
		RootfsPath:   v.RootfsPath,
		TapName:      v.TapName,
		MacAddress:   v.MacAddress,
		Bridge:       v.Bridge,
		ConsoleSock:  v.ConsoleSock,
		PtsPath:      v.PtsPath,
		PID:          v.PID,
		CreatedAt:    v.CreatedAt,
		StartedAt:    v.StartedAt,
	}
}

type errorBody struct {
	Error string `json:"error"`
}
