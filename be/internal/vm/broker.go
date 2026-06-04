package vm

import (
	"context"
	"io"
	"net"
)

// Broker fans VM serial output out to N subscribers (Unix-socket clients,
// PTY master, WebSocket handlers) and merges input from any subscriber back
// into the VM. It is a straight port of the broker that lived in
// cmd/main.go and has the same multi-client semantics as
//
//	qemu -serial unix:/tmp/console.sock,server,nowait
//
// minus the QEMU-specific framing.
type Broker struct {
	vmOut io.Reader // read VM stdout
	vmIn  io.Writer // write VM stdin

	subscribe   chan chan []byte
	unsubscribe chan chan []byte
	incoming    chan []byte // data from any subscriber → VM
	publish     chan []byte // data from VM → all subscribers
}

func NewBroker(vmOut io.Reader, vmIn io.Writer) *Broker {
	return &Broker{
		vmOut:       vmOut,
		vmIn:        vmIn,
		subscribe:   make(chan chan []byte, 16),
		unsubscribe: make(chan chan []byte, 16),
		incoming:    make(chan []byte, 64),
		publish:     make(chan []byte, 64),
	}
}

// Subscribe returns a channel of VM output bytes. The caller must call
// Unsubscribe when done; otherwise the broker will keep buffering for it.
func (b *Broker) Subscribe() chan []byte {
	ch := make(chan []byte, 64)
	b.subscribe <- ch
	return ch
}

func (b *Broker) Unsubscribe(ch chan []byte) {
	b.unsubscribe <- ch
}

// Send pushes bytes from a subscriber back into the VM. It is non-blocking
// from the caller's perspective beyond the bounded buffer.
func (b *Broker) Send(ctx context.Context, data []byte) {
	cp := make([]byte, len(data))
	copy(cp, data)
	select {
	case b.incoming <- cp:
	case <-ctx.Done():
	}
}

// Run is the central event loop. Returns when ctx is cancelled.
func (b *Broker) Run(ctx context.Context) {
	clients := make(map[chan []byte]struct{})

	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := b.vmOut.Read(buf)
			if n > 0 {
				data := make([]byte, n)
				copy(data, buf[:n])
				select {
				case b.publish <- data:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	go func() {
		for {
			select {
			case data := <-b.incoming:
				_, _ = b.vmIn.Write(data)
			case <-ctx.Done():
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			for ch := range clients {
				close(ch)
			}
			return

		case ch := <-b.subscribe:
			clients[ch] = struct{}{}

		case ch := <-b.unsubscribe:
			if _, ok := clients[ch]; ok {
				delete(clients, ch)
				close(ch)
			}

		case data := <-b.publish:
			for ch := range clients {
				select {
				case ch <- data:
				default:
					// slow client — drop rather than block VM output
				}
			}
		}
	}
}

// AcceptUnix accepts incoming Unix socket connections and bridges each one
// into the broker. Blocks until the listener is closed or ctx is cancelled.
func (b *Broker) AcceptUnix(ctx context.Context, l net.Listener) {
	for {
		conn, err := l.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				return
			}
		}
		go b.handleConn(ctx, conn)
	}
}

func (b *Broker) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	ch := b.Subscribe()
	defer b.Unsubscribe(ch)

	go func() {
		for data := range ch {
			if _, err := conn.Write(data); err != nil {
				conn.Close()
				return
			}
		}
	}()

	buf := make([]byte, 256)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			b.Send(ctx, buf[:n])
		}
		if err != nil {
			return
		}
	}
}
