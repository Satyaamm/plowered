package outbox

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"
)

// NATSPublisher is a deliberately minimal NATS publisher that speaks the
// core PUB protocol over a TCP connection. We don't pull in the full
// nats.go client because:
//   - it would expand go.mod (and the worker is already heavy);
//   - we only need fire-and-forget PUB; durability lives in the outbox
//     table itself, not in JetStream;
//   - the client lazy-reconnects on every tick, so a NATS outage just
//     leaves rows un-processed (the relay retries them on the next tick).
//
// Subject convention: `plowered.outbox.<aggregate_type>.<event_type>`.
// JetStream consumers subscribe with a wildcard — `plowered.outbox.>`.
//
// NB: this is intentionally small enough that a future swap to nats.go
// (with JetStream + ack semantics) is a one-file change.
type NATSPublisher struct {
	URL     string
	Subject string // optional override; defaults to derive from event

	mu   sync.Mutex
	conn net.Conn
	rw   *bufio.ReadWriter
}

// NewNATSPublisher returns a publisher that connects lazily on first
// Publish. URL is parsed via net/url; only nats:// is supported.
func NewNATSPublisher(natsURL string) *NATSPublisher {
	return &NATSPublisher{URL: natsURL}
}

func (p *NATSPublisher) Publish(ctx context.Context, e *Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.ensureConn(ctx); err != nil {
		return err
	}
	subj := p.Subject
	if subj == "" {
		subj = fmt.Sprintf("plowered.outbox.%s.%s",
			sanitizeSubj(e.AggregateType), sanitizeSubj(e.EventType))
	}
	body, err := json.Marshal(e)
	if err != nil {
		return err
	}
	cmd := fmt.Sprintf("PUB %s %d\r\n", subj, len(body))
	if _, err := p.rw.WriteString(cmd); err != nil {
		p.dropConn()
		return err
	}
	if _, err := p.rw.Write(body); err != nil {
		p.dropConn()
		return err
	}
	if _, err := p.rw.WriteString("\r\n"); err != nil {
		p.dropConn()
		return err
	}
	if err := p.rw.Flush(); err != nil {
		p.dropConn()
		return err
	}
	return nil
}

// ensureConn dials NATS if we don't yet have a connection. The
// CONNECT/INFO handshake is skipped — modern nats-server accepts
// unauthenticated PUB on a tcp connect with no preamble.
func (p *NATSPublisher) ensureConn(ctx context.Context) error {
	if p.conn != nil {
		return nil
	}
	u, err := url.Parse(p.URL)
	if err != nil {
		return fmt.Errorf("outbox/nats: parse url: %w", err)
	}
	if u.Scheme != "nats" {
		return fmt.Errorf("outbox/nats: unsupported scheme %q", u.Scheme)
	}
	host := u.Host
	if !strings.Contains(host, ":") {
		host += ":4222"
	}
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", host)
	if err != nil {
		return fmt.Errorf("outbox/nats: dial: %w", err)
	}
	// Read and discard the INFO line — nats-server sends it before any
	// commands. We don't authenticate; relying on the server allowing
	// unauthenticated PUB (default in dev).
	br := bufio.NewReader(conn)
	if _, err := br.ReadString('\n'); err != nil {
		conn.Close()
		return fmt.Errorf("outbox/nats: read INFO: %w", err)
	}
	bw := bufio.NewWriter(conn)
	if _, err := bw.WriteString("CONNECT {}\r\n"); err != nil {
		conn.Close()
		return err
	}
	if err := bw.Flush(); err != nil {
		conn.Close()
		return err
	}
	p.conn = conn
	p.rw = bufio.NewReadWriter(br, bw)
	return nil
}

func (p *NATSPublisher) dropConn() {
	if p.conn != nil {
		_ = p.conn.Close()
	}
	p.conn = nil
	p.rw = nil
}

// Close closes the underlying TCP connection. Safe to call repeatedly.
func (p *NATSPublisher) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.dropConn()
	return nil
}

// sanitizeSubj keeps NATS subjects clean: no spaces, dots, or > / *.
func sanitizeSubj(s string) string {
	if s == "" {
		return "unknown"
	}
	r := strings.NewReplacer(" ", "_", ".", "_", ">", "_", "*", "_")
	return r.Replace(s)
}
