// Package store — JetStream stream provisioning.
//
// Gateway startup is the SINGLE SOURCE OF TRUTH for stream provisioning.
// EnsureStreams is called once at boot before the HTTP server starts.
// Failure causes the gateway to exit(1) — never serve without streams.
//
// The P7 mio-jetstream-bootstrap Job is VERIFICATION-ONLY: it asserts
// streams exist with locked config and exits non-zero otherwise. It
// NEVER creates or mutates streams. This prevents two-writer races.
//
// Single-replica constraint: AddOrUpdateStream is idempotent on identical
// config, but two replicas booting with disagreeing config could ping-pong.
// POC runs single replica (maxSurge:0, maxUnavailable:1) until P7 resolves.
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

const (
	StreamInbound  = "MESSAGES_INBOUND"
	StreamOutbound = "MESSAGES_OUTBOUND"
)

// EnsureStreams provisions MESSAGES_INBOUND and MESSAGES_OUTBOUND with
// the locked configuration. Idempotent — safe to call on every boot.
func EnsureStreams(ctx context.Context, js jetstream.JetStream, natsReplicas int) error {
	if err := ensureInbound(ctx, js, natsReplicas); err != nil {
		return fmt.Errorf("store: ensure stream MESSAGES_INBOUND: %w", err)
	}
	if err := ensureOutbound(ctx, js, natsReplicas); err != nil {
		return fmt.Errorf("store: ensure stream MESSAGES_OUTBOUND: %w", err)
	}
	return nil
}

func ensureInbound(ctx context.Context, js jetstream.JetStream, replicas int) error {
	cfg := jetstream.StreamConfig{
		Name:        StreamInbound,
		Subjects:    []string{"mio.inbound.>"},
		Retention:   jetstream.LimitsPolicy,
		MaxAge:      7 * 24 * time.Hour, // 7 days; GCS sink archives beyond this
		Storage:     jetstream.FileStorage,
		Replicas:    replicas,
		Duplicates:  2 * time.Minute, // NATS-side dedup window for Nats-Msg-Id
		Description: "Inbound messages from all channel adapters (gateway-authoritative)",
	}
	_, err := js.CreateOrUpdateStream(ctx, cfg)
	return err
}

func ensureOutbound(ctx context.Context, js jetstream.JetStream, replicas int) error {
	cfg := jetstream.StreamConfig{
		Name:        StreamOutbound,
		Subjects:    []string{"mio.outbound.>"},
		Retention:   jetstream.WorkQueuePolicy, // consumed once by sender pool
		MaxAge:      24 * time.Hour,
		Storage:     jetstream.FileStorage,
		Replicas:    replicas,
		Duplicates:  60 * time.Second,
		Description: "Outbound send commands (gateway-authoritative)",
	}
	_, err := js.CreateOrUpdateStream(ctx, cfg)
	return err
}

// ConsumerSenderPool is the durable name for the gateway sender-pool consumer.
const ConsumerSenderPool = "sender-pool"

// EnsureSenderConsumer provisions the durable pull consumer on MESSAGES_OUTBOUND
// that the sender pool attaches to. Idempotent — safe to call on every boot.
//
// Config choices:
//   - MaxAckPending=32: allows 32 in-flight messages across the worker pool.
//   - AckWait=30s:       matches sdk.WithAckWait in main.go.
//   - MaxDeliver=5:      default; adapters may override per-channel via MaxDeliver().
//   - FilterSubject="":  consume all outbound subjects (mio.outbound.>).
func EnsureSenderConsumer(ctx context.Context, js jetstream.JetStream) error {
	cfg := jetstream.ConsumerConfig{
		Name:          ConsumerSenderPool,
		Durable:       ConsumerSenderPool,
		FilterSubject: "",       // all subjects on MESSAGES_OUTBOUND
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       30 * time.Second,
		MaxAckPending: 32,
		MaxDeliver:    5,
		DeliverPolicy: jetstream.DeliverAllPolicy,
		Description:   "Gateway sender-pool pull consumer (gateway-authoritative)",
	}
	_, err := js.CreateOrUpdateConsumer(ctx, StreamOutbound, cfg)
	if err != nil {
		return fmt.Errorf("store: ensure sender-pool consumer: %w", err)
	}
	return nil
}
