# firecracker-manager-go

A small HTTP API daemon for managing many [Firecracker](https://firecracker-microvm.github.io/) microVMs on a single host.

## What it does

- Exposes a localhost-only REST API (Fiber v2) for VM lifecycle: create, start, stop (graceful Ctrl+Alt+Del), kill, restart, delete, list, status.
- Auto-creates a `tap` device per VM and attaches it to a pre-existing Linux bridge. You run DHCP on the bridge yourself.
- Provisions each VM's rootfs by copying a configured raw `.img` template and growing it (sparsely) to the requested size. Guest is responsible for `resize2fs`.
- Persists VM definitions through a pluggable `Store` interface. JSON-file backend ships today; a MongoDB backend stub is wired in and ready to be filled in later without touching the manager or API code.
- Exposes each VM's serial console three ways simultaneously (one shared fan-out broker per VM):
  1. Unix socket  — `socat -,raw,echo=0 UNIX-CONNECT:<state_dir>/vms/<id>/console.sock`
  2. PTY          — `screen <pts_path from GET /vms/:id>`
  3. WebSocket    — `ws://<listen_addr>/vms/<id>/console` (xterm.js / websocat compatible)

## Prerequisites

- Linux host with `firecracker` (>= v1.x) on `$PATH`.
- Root privileges (to create tap devices and attach them to the bridge).
- A pre-existing bridge (e.g. `br0`) with a DHCP server bound to it.
- A Linux kernel binary suitable for Firecracker (e.g. `vmlinux`).
- A raw `.img` rootfs template.

## Build

```
go build ./...
```

The daemon binary is `./manager` (build with `go build -o manager ./cmd/manager`).

## Configure

Copy `configs/manager.yaml.example` and edit:

```yaml
listen_addr: "127.0.0.1:8080"
bridge_name: "br0"
state_dir:  "/var/lib/firecracker-manager"
kernel_path: "/path/to/vmlinux"
rootfs_template: "/path/to/rootfs.img"
default_disk_size_mib: 2048
store:
  type: "json"
```

Any field can be overridden by env var: `MANAGER_LISTEN_ADDR`, `MANAGER_BRIDGE_NAME`, `MANAGER_STATE_DIR`, `MANAGER_KERNEL_PATH`, `MANAGER_ROOTFS_TEMPLATE`, `MANAGER_STORE_TYPE`, `MANAGER_STORE_JSON_PATH`, `MANAGER_STORE_MONGO_URI`, `MANAGER_DEFAULT_DISK_SIZE_MIB`.

## Run

```
sudo ./manager -config configs/manager.yaml
```

VMs whose last `desired_state` was `running` are auto-relaunched on startup.

## API

| Method | Path                  | Body / behaviour                                   |
|--------|-----------------------|----------------------------------------------------|
| GET    | `/healthz`            | `{ "ok": true }`                                   |
| POST   | `/vms`                | `{ "name", "vcpus?", "mem_mib?", "disk_size_mib?", "kernel_args?" }` → 201 VM |
| GET    | `/vms`                | `[VM, ...]`                                        |
| GET    | `/vms/:id`            | VM                                                 |
| DELETE | `/vms/:id`            | 204 (force-stops + removes tap, rootfs, definition)|
| POST   | `/vms/:id/start`      | VM                                                 |
| POST   | `/vms/:id/stop`       | VM (Ctrl+Alt+Del; falls back to kill after 15 s)   |
| POST   | `/vms/:id/kill`       | VM (force `StopVMM`)                               |
| POST   | `/vms/:id/restart`    | VM                                                 |
| GET    | `/vms/:id/console`    | WebSocket upgrade — bidirectional console bytes    |

### Quick smoke test

```
curl -X POST localhost:8080/vms -d '{"name":"alpha","disk_size_mib":4096}' -H 'content-type: application/json'
# → {"id":"01J...","state":"created", ...}

curl -X POST localhost:8080/vms/<id>/start
# → {"state":"running","pts_path":"/dev/pts/N", ...}

socat -,raw,echo=0 UNIX-CONNECT:/var/lib/firecracker-manager/vms/<id>/console.sock
screen /dev/pts/N
websocat ws://localhost:8080/vms/<id>/console

curl -X POST localhost:8080/vms/<id>/stop
curl -X DELETE localhost:8080/vms/<id>
```

## Storage backend

`internal/store/store.go` defines:

```go
type Store interface {
    Get(ctx, id) (*vm.VM, error)
    List(ctx) ([]*vm.VM, error)
    Put(ctx, *vm.VM) error
    Delete(ctx, id) error
    Close() error
}
```

The manager only uses this interface. `cmd/manager/main.go` selects a concrete impl by `cfg.Store.Type` (`json` or `mongo`). To enable MongoDB later, fill in `internal/store/mongostore/mongostore.go` and flip `store.type` in the config — no other code changes.

## Layout

```
cmd/manager/        daemon entrypoint
internal/api/       Fiber app, handlers, WebSocket console
internal/manager/   in-memory registry, lifecycle orchestration, reconcile
internal/vm/        VM struct, broker (fan-out), runtime (firecracker SDK glue)
internal/store/     Store interface
internal/store/jsonstore/   file-backed impl (atomic temp+rename, flock)
internal/store/mongostore/  stub for future MongoDB impl
internal/network/   tap + bridge via netlink, deterministic tap/MAC from VM-ID
internal/rootfs/    template copy + os.Truncate grow
internal/config/    YAML + env config loader
configs/            example config
```

## Caveats / not implemented

- Manager process death = VM death. Persisted definitions survive; running VMs do not. Reconcile relaunches them on next start.
- Disk grow only enlarges the block device. Run `resize2fs` (or fs equivalent) inside the guest.
- No auth — bind to `127.0.0.1` only.
- No snapshots / pause / resume / vsock / MMDS / multi-disk yet.
- COW/overlay rootfs not implemented; full copy each time.
