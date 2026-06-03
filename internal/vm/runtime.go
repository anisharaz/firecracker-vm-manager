package vm

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/anish-araz_cumulus/firecracker-manager-go/internal/network"
	"github.com/creack/pty"
	firecracker "github.com/firecracker-microvm/firecracker-go-sdk"
	models "github.com/firecracker-microvm/firecracker-go-sdk/client/models"
)

// Runtime holds all live, non-persisted state attached to a running VM:
// the firecracker SDK Machine handle, console plumbing, and cancel funcs.
type Runtime struct {
	machine     *firecracker.Machine
	cancel      context.CancelFunc
	broker      *Broker
	listener    net.Listener
	ptyMaster   *os.File
	socketPath  string
	apiSockPath string
	stdinR      *os.File
	stdinW      *os.File
	stdoutR     *os.File
	stdoutW     *os.File
}

// VMDir returns the per-VM directory under the manager state dir.
func VMDir(stateDir, id string) string {
	return filepath.Join(stateDir, "vms", id)
}

// Start launches the firecracker process for v, sets up tap, broker, console
// surfaces (Unix socket + PTY), and stores runtime handles on v.Runtime.
//
// Caller must hold v.Mu.
func Start(parent context.Context, v *VM, stateDir, bridge string) error {
	if v.Runtime != nil {
		return fmt.Errorf("vm %s: already running", v.ID)
	}

	dir := VMDir(stateDir, v.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("vm %s: mkdir state: %w", v.ID, err)
	}

	apiSock := filepath.Join(dir, "firecracker.sock")
	consoleSock := filepath.Join(dir, "console.sock")
	_ = os.Remove(apiSock)
	_ = os.Remove(consoleSock)

	if err := network.CreateTap(v.TapName, bridge, v.MacAddress); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(parent)

	listener, err := net.Listen("unix", consoleSock)
	if err != nil {
		cancel()
		_ = network.DeleteTap(v.TapName)
		return fmt.Errorf("vm %s: listen console sock: %w", v.ID, err)
	}

	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		cancel()
		listener.Close()
		_ = network.DeleteTap(v.TapName)
		return fmt.Errorf("vm %s: stdin pipe: %w", v.ID, err)
	}
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		cancel()
		listener.Close()
		stdinR.Close()
		stdinW.Close()
		_ = network.DeleteTap(v.TapName)
		return fmt.Errorf("vm %s: stdout pipe: %w", v.ID, err)
	}

	broker := NewBroker(stdoutR, stdinW)
	go broker.Run(ctx)
	go broker.AcceptUnix(ctx, listener)

	// PTY: open a new master/slave pair and bridge the master to the broker
	// so `screen <pts>` works exactly like socat on the unix socket.
	ptm, pts, err := pty.Open()
	if err != nil {
		cancel()
		listener.Close()
		stdinR.Close()
		stdinW.Close()
		stdoutR.Close()
		stdoutW.Close()
		_ = network.DeleteTap(v.TapName)
		return fmt.Errorf("vm %s: pty open: %w", v.ID, err)
	}
	ptsName := pts.Name()
	pts.Close() // we don't use the slave here; clients open it via name

	go bridgePTYToBroker(ctx, broker, ptm)

	cmd := firecracker.VMCommandBuilder{}.
		WithBin("firecracker").
		WithSocketPath(apiSock).
		Build(ctx)
	cmd.Stdin = stdinR
	cmd.Stdout = stdoutW
	cmd.Stderr = os.Stderr

	cfg := firecracker.Config{
		SocketPath:      apiSock,
		KernelImagePath: v.KernelPath,
		KernelArgs:      v.KernelArgs,
		Drives: []models.Drive{
			{
				DriveID:      firecracker.String("rootfs"),
				PathOnHost:   firecracker.String(v.RootfsPath),
				IsRootDevice: firecracker.Bool(true),
				IsReadOnly:   firecracker.Bool(false),
			},
		},
		NetworkInterfaces: firecracker.NetworkInterfaces{
			{
				StaticConfiguration: &firecracker.StaticNetworkConfiguration{
					HostDevName: v.TapName,
					MacAddress:  v.MacAddress,
				},
			},
		},
		MachineCfg: models.MachineConfiguration{
			VcpuCount:  firecracker.Int64(int64(v.VCPUs)),
			MemSizeMib: firecracker.Int64(int64(v.MemMiB)),
			Smt:        firecracker.Bool(false),
		},
	}

	m, err := firecracker.NewMachine(ctx, cfg, firecracker.WithProcessRunner(cmd))
	if err != nil {
		cancel()
		listener.Close()
		stdinR.Close()
		stdinW.Close()
		stdoutR.Close()
		stdoutW.Close()
		ptm.Close()
		_ = network.DeleteTap(v.TapName)
		return fmt.Errorf("vm %s: new machine: %w", v.ID, err)
	}

	if err := m.Start(ctx); err != nil {
		cancel()
		listener.Close()
		stdinR.Close()
		stdinW.Close()
		stdoutR.Close()
		stdoutW.Close()
		ptm.Close()
		_ = network.DeleteTap(v.TapName)
		return fmt.Errorf("vm %s: start: %w", v.ID, err)
	}

	// Caller-side ends of the pipes are owned by firecracker now.
	stdinR.Close()
	stdoutW.Close()

	if cmd.Process != nil {
		v.PID = cmd.Process.Pid
	}
	v.PtsPath = ptsName
	v.ConsoleSock = consoleSock
	v.StartedAt = time.Now()
	v.State = StateRunning
	v.Runtime = &Runtime{
		machine:     m,
		cancel:      cancel,
		broker:      broker,
		listener:    listener,
		ptyMaster:   ptm,
		socketPath:  consoleSock,
		apiSockPath: apiSock,
		stdinR:      stdinR,
		stdinW:      stdinW,
		stdoutR:     stdoutR,
		stdoutW:     stdoutW,
	}
	return nil
}

func bridgePTYToBroker(ctx context.Context, b *Broker, ptm *os.File) {
	ch := b.Subscribe()
	defer b.Unsubscribe(ch)

	go func() {
		for data := range ch {
			if _, err := ptm.Write(data); err != nil {
				return
			}
		}
	}()

	buf := make([]byte, 256)
	for {
		n, err := ptm.Read(buf)
		if n > 0 {
			b.Send(ctx, buf[:n])
		}
		if err != nil {
			return
		}
	}
}

// Stop attempts a graceful shutdown via Ctrl+Alt+Del, then force-kills
// after the timeout if firecracker is still running.
//
// Caller must hold v.Mu.
func Stop(ctx context.Context, v *VM, timeout time.Duration) error {
	if v.Runtime == nil {
		return nil
	}
	v.State = StateStopping
	rt := v.Runtime

	if err := rt.machine.Shutdown(ctx); err != nil {
		// fall through to kill
	}

	done := make(chan error, 1)
	go func() { done <- rt.machine.Wait(ctx) }()

	select {
	case <-done:
	case <-time.After(timeout):
		_ = rt.machine.StopVMM()
		<-done
	}
	cleanup(v)
	v.State = StateStopped
	return nil
}

// Kill force-stops the VMM and tears down resources.
//
// Caller must hold v.Mu.
func Kill(ctx context.Context, v *VM) error {
	if v.Runtime == nil {
		return nil
	}
	v.State = StateStopping
	_ = v.Runtime.machine.StopVMM()
	_ = v.Runtime.machine.Wait(ctx)
	cleanup(v)
	v.State = StateStopped
	return nil
}

// cleanup releases all per-VM runtime resources. Caller must hold v.Mu.
func cleanup(v *VM) {
	rt := v.Runtime
	if rt == nil {
		return
	}
	rt.cancel()
	if rt.listener != nil {
		rt.listener.Close()
	}
	if rt.ptyMaster != nil {
		rt.ptyMaster.Close()
	}
	closeFile(rt.stdinW)
	closeFile(rt.stdoutR)
	if rt.socketPath != "" {
		_ = os.Remove(rt.socketPath)
	}
	if rt.apiSockPath != "" {
		_ = os.Remove(rt.apiSockPath)
	}
	_ = network.DeleteTap(v.TapName)
	v.PID = 0
	v.PtsPath = ""
	v.Runtime = nil
}

func closeFile(f *os.File) {
	if f != nil {
		_ = f.Close()
	}
}

// SubscribeConsole exposes the broker for API-level integrations (WebSocket).
// Returns nil if the VM is not running.
func SubscribeConsole(v *VM) (*Broker, error) {
	if v.Runtime == nil {
		return nil, fmt.Errorf("vm %s: not running", v.ID)
	}
	return v.Runtime.broker, nil
}

// compile-time ref to silence unused imports during partial builds
var _ = io.Copy
