// Package nats embeds a NATS JetStream server in-process for the all-in-one
// binary. The same sdk-go client connects to either an embedded server or
// an external cluster — only the bootstrap differs.
//
// Embedded JetStream supports both memory + file storage. Memory is fast
// + volatile; file survives process restart. The all-in-one binary picks
// via the --storage flag.
//
// Production deploys MUST use the external NATS cluster — embedded NATS is
// single-node and has no replication. The all-in-one binary refuses to
// start with --storage=memory in MIO_ENV=prod (see cmd/all-in-one/main.go).
package nats

import (
	"fmt"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
)

// EmbeddedOpts configures the in-process JetStream server. Defaults are
// chosen so a laptop run boots without any tuning; production users set
// MaxFile / MaxMemory + a real StoreDir.
type EmbeddedOpts struct {
	// Storage is "memory" or "file". Memory: StoreDir ignored.
	Storage string
	// StoreDir is the on-disk root for file storage (ignored for memory).
	StoreDir string
	// MaxMemory caps the per-stream memory budget (bytes). 0 = no cap.
	MaxMemory int64
	// MaxFile caps the per-stream file budget (bytes). 0 = no cap.
	MaxFile int64
	// Host / Port: bind address. Port 0 = OS-assigned (use for tests).
	Host string
	Port int
}

// StartEmbedded boots the embedded server and returns the running handle
// plus the client URL the SDK should dial. Caller MUST defer ns.Shutdown()
// to release the port + flush data on file storage.
//
// Readiness is checked synchronously (waits up to 5s for JetStream to be
// fully ready). The 5s ceiling matches the SDK's connect timeout so a
// laptop boot races with NATS startup gracefully.
func StartEmbedded(opts EmbeddedOpts) (*natsserver.Server, string, error) {
	host := opts.Host
	if host == "" {
		host = "127.0.0.1"
	}
	port := opts.Port
	if port == 0 {
		port = -1 // OS-assigned
	}

	jsOpts := &natsserver.Options{
		Host:               host,
		Port:               port,
		JetStream:          true,
		StoreDir:           opts.StoreDir,
		JetStreamMaxMemory: opts.MaxMemory,
		JetStreamMaxStore:  opts.MaxFile,
		Debug:              false,
		NoLog:              true,
		NoSigs:             true,
		// Sync interval — 10s in tests + dev keeps durability tests deterministic.
		// Production using file storage should still expect to lose ≤2 min on
		// an ungraceful kill if using defaults; document loudly.
		SyncInterval: 10 * time.Second,
	}
	if opts.Storage == "memory" {
		// JetStream still requires a store dir for streams that opt into
		// file; passing empty keeps it tmp-ish. Streams explicitly created
		// with `Storage: MemoryStorage` ignore disk entirely.
		jsOpts.StoreDir = ""
	}

	ns, err := natsserver.NewServer(jsOpts)
	if err != nil {
		return nil, "", fmt.Errorf("nats: new server: %w", err)
	}
	go ns.Start()
	if !ns.ReadyForConnections(5 * time.Second) {
		ns.Shutdown()
		return nil, "", fmt.Errorf("nats: server not ready after 5s")
	}
	return ns, ns.ClientURL(), nil
}
