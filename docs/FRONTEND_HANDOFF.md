# Frontend Handoff — firecracker-manager-go

> Drop this file into your Next.js project as `.github/copilot-instructions.md`
> (or `AGENTS.md`). It tells Copilot/Claude everything needed to build the UI
> against the backend HTTP API. **No backend code lives here** — only the
> contract, conventions, and constraints the frontend has to honor.

---

## What you're building
A web UI for managing [Firecracker](https://firecracker-microvm.github.io/) microVMs through a Go HTTP daemon (`firecracker-manager-go`). The backend is single-host, localhost-only, no auth.

Stack expected: **Next.js (App Router) + TypeScript + Tailwind + shadcn/ui**. Use `fetch` (or your preferred client lib) directly against the API. For the live VM serial console, use `xterm.js` + a native `WebSocket`.

## Backend base URL
- Default: `http://localhost:8080`
- Configurable via `NEXT_PUBLIC_MANAGER_URL`. WebSocket origin derives from the same host (swap `http`→`ws`).
- The backend binds to `127.0.0.1` only. For dev, run Next.js on the same host.

## Auth
None. Do not implement auth flows. The daemon is intended for trusted, local use.

---

## REST API contract

All bodies and responses are JSON. Errors come back as `{ "error": "<message>" }` with the appropriate HTTP status.

### Health
```
GET /healthz  →  200  { "ok": true }
```

### List VMs
```
GET /vms  →  200  VM[]
```

### Create VM
```
POST /vms
Content-Type: application/json
{
  "name": "alpha",                 // required
  "vcpus": 2,                      // optional, server default
  "mem_mib": 1024,                 // optional, server default
  "disk_size_mib": 4096,           // optional, server default; must be >= template size
  "kernel_args": "console=ttyS0 …" // optional
}
→ 201 VM
```

Validation errors → `400 { "error": "..." }`.

### Get VM
```
GET /vms/:id  →  200 VM | 404
```

### Lifecycle
```
POST /vms/:id/start    →  200 VM
POST /vms/:id/stop     →  200 VM   // graceful: Ctrl+Alt+Del; falls back to kill after 15 s
POST /vms/:id/kill     →  200 VM   // force StopVMM
POST /vms/:id/restart  →  200 VM
DELETE /vms/:id        →  204      // force-stops + removes tap, rootfs, definition
```

### VM object (response shape)
```ts
type VMState        = "created" | "starting" | "running" | "stopping" | "stopped" | "failed";
type VMDesiredState = "stopped" | "running";

interface VM {
  id: string;                  // ULID, used everywhere as identifier
  name: string;
  state: VMState;
  desired_state: VMDesiredState;

  vcpus: number;
  mem_mib: number;
  disk_size_mib: number;
  kernel_path: string;
  kernel_args: string;
  rootfs_path: string;

  tap_name: string;
  mac_address: string;
  bridge: string;

  console_sock: string;        // Unix socket path on host (server-side use only)
  pts_path?: string;           // /dev/pts/N when running
  pid?: number;                // firecracker PID, only when running

  created_at: string;          // RFC3339
  started_at?: string;         // RFC3339, only when running
}
```

---

## Serial console (WebSocket)

```
GET ws://<host>/vms/:id/console
```

- Opens a bidirectional WebSocket.
- Server frames are **binary** (`Uint8Array`). Each frame is whatever bytes the VM produced since the last read; do not assume one frame = one line.
- Client → server frames: send keystrokes as **binary**. Send raw bytes; do not append `\n` for keys other than Enter (Enter = `\r` or `\n`).
- The server fans this stream out alongside a Unix socket and a PTY, so multiple browser tabs can attach concurrently to the same VM and all see the same output.
- Use `xterm.js`. Wire it like this (sketch):

```ts
const term = new Terminal({ convertEol: true });
const ws = new WebSocket(`${wsBase}/vms/${id}/console`);
ws.binaryType = "arraybuffer";

ws.onmessage = (e) => term.write(new Uint8Array(e.data as ArrayBuffer));
term.onData((data) => ws.readyState === WebSocket.OPEN && ws.send(new TextEncoder().encode(data)));
```

Reconnect logic: if `ws.onclose` fires, the VM either stopped or the manager restarted. Retry with backoff; surface `state` from `GET /vms/:id`.

---

## Required UI surfaces (suggested)

1. **VM list page (`/`)** — table of VMs with state badge, quick actions (start/stop/kill/restart/delete), refresh.
2. **VM detail page (`/vms/[id]`)** — full metadata, resource specs, network info (tap/MAC/bridge), and a tabbed view: "Overview" / "Console" / "JSON".
3. **Create VM modal/page** — form for `name`, `vcpus`, `mem_mib`, `disk_size_mib`, `kernel_args`.
4. **Console tab** — full-height `xterm.js` panel with reconnect button and a "send Ctrl+Alt+Del" shortcut (just send the bytes; the kernel handles it).

State transitions reference (for badges and disabling buttons):

| current        | start | stop | kill | restart | delete |
|----------------|-------|------|------|---------|--------|
| created        | ✓     | —    | —    | —       | ✓      |
| starting       | —     | —    | ✓    | —       | —      |
| running        | —     | ✓    | ✓    | ✓       | ✓      |
| stopping       | —     | —    | ✓    | —       | —      |
| stopped/failed | ✓     | —    | —    | —       | ✓      |

---

## Conventions / non-goals

- **Use the API as-is.** Do not invent fields. Do not assume pagination, filtering, or sorting on the server — do those client-side.
- **Polling is fine.** No SSE/long-poll/streaming for state today. Refresh the list every 3–5 s while the page is open; refetch immediately after issuing a lifecycle action and reconcile the returned VM into local state.
- **Optimistic UI:** safe for `delete` only. For start/stop/restart the server response carries the authoritative new state — wait for it.
- **Errors:** parse `{error}` field; surface in a toast. 404 means the VM was deleted out from under you — drop it from local state.
- **Long fields:** `id` (ULID, 26 chars) and paths can be long; truncate with tooltips.
- **Time:** treat `created_at` / `started_at` as UTC ISO-8601.

## Local dev recipe

1. Run the backend (`sudo ./main start -c configs/manager.yaml`) on `:8080`.
2. In the Next.js project: `npm run dev` on `:3000`.
3. Use `next.config.js` rewrites to avoid CORS in dev:

```js
async rewrites() {
  return [{ source: "/api/:path*", destination: "http://127.0.0.1:8080/:path*" }];
}
```

For the WebSocket, point directly at `ws://127.0.0.1:8080/vms/:id/console` from the browser (Next rewrites don't proxy WebSockets cleanly).

## Things explicitly out of scope (do not invent UI for these)

- Auth / users / RBAC
- Snapshot / pause / resume
- vsock / MMDS / multi-disk / GPU
- Bridge or DHCP configuration (operator-managed, outside the API)
- Rootfs templates management (single template path is in the daemon's YAML)
