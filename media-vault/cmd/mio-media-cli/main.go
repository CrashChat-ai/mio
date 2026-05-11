// Command mio-media-cli operates on attachment storage outside the
// sidecar process: lists, stats, deletes by account_id (GDPR), issues signed
// URLs, and applies bucket lifecycle rules.
//
// Subcommands:
//
//	list           --prefix=mio/attachments/zoho_cliq/
//	stat           <key>
//	delete         (--account_id | --tenant_id | --conversation_id)=<uuid>
//	                  [--dry-run] [--concurrency=10] [--prefix=...]
//	signed-url     <key> [--ttl=1h]
//	set-lifecycle  [--age-days=7] [--prefix=mio/attachments/]
//
// Reuses the storage.New(ctx) factory; runs locally with ADC creds or
// in-cluster as a Job using the mio-attachments GSA.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/crashchat-ai/mio/media-vault/internal/gdpr"
	"github.com/crashchat-ai/mio/media-vault/internal/lifecycle"
	"github.com/crashchat-ai/mio/media-vault/internal/storage"
	_ "github.com/crashchat-ai/mio/media-vault/internal/storage/gcs"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	store, err := storage.New(ctx)
	if err != nil {
		log.Error("storage init", "err", err)
		os.Exit(1)
	}
	if closer, ok := store.(interface{ Close() error }); ok {
		defer func() { _ = closer.Close() }()
	}

	switch cmd {
	case "list":
		os.Exit(runList(ctx, store, args, log))
	case "stat":
		os.Exit(runStat(ctx, store, args, log))
	case "delete":
		os.Exit(runDelete(ctx, store, args, log))
	case "signed-url":
		os.Exit(runSignedURL(ctx, store, args, log))
	case "set-lifecycle":
		os.Exit(runSetLifecycle(ctx, store, args, log))
	default:
		printUsage()
		os.Exit(2)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `mio-media-cli — operate on persisted attachment bytes.

Commands:
  list           --prefix=...
  stat           <key>
  delete         (--account_id | --tenant_id | --conversation_id)=<uuid>
                    [--dry-run] [--concurrency=10] [--prefix=...]
  signed-url     <key> [--ttl=1h]
  set-lifecycle  [--age-days=7] [--prefix=mio/attachments/]

Env:
  MIO_STORAGE_BACKEND=gcs|s3   (default gcs)
  MIO_STORAGE_BUCKET=<name>    (required)`)
}

func runList(ctx context.Context, s storage.Storage, args []string, log *slog.Logger) int {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	prefix := fs.String("prefix", "mio/attachments/", "key prefix")
	_ = fs.Parse(args)

	// Columns: key, size, sha256, tenant_id, account_id, conversation_id, source_message_id.
	out, errCh := s.List(ctx, *prefix)
	for o := range out {
		fmt.Printf("%s\t%d\t%s\t%s\t%s\t%s\t%s\n",
			o.Key, o.Size, o.SHA256Hex,
			o.TenantID, o.AccountID, o.ConversationID, o.SourceMessageID)
	}
	if err := <-errCh; err != nil {
		log.Error("list failed", "err", err)
		return 1
	}
	return 0
}

func runStat(ctx context.Context, s storage.Storage, args []string, log *slog.Logger) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: stat <key>")
		return 2
	}
	o, err := s.Stat(ctx, args[0])
	if err != nil {
		log.Error("stat failed", "err", err)
		return 1
	}
	fmt.Printf(
		"Key:             %s\n"+
			"Size:            %d\n"+
			"ContentType:     %s\n"+
			"SHA256:          %s\n"+
			"TenantID:        %s\n"+
			"AccountID:       %s\n"+
			"ConversationID:  %s\n"+
			"SourceMessageID: %s\n"+
			"Modified:        %s\n",
		o.Key, o.Size, o.ContentType, o.SHA256Hex,
		o.TenantID, o.AccountID, o.ConversationID, o.SourceMessageID,
		o.ModifiedAt.Format(time.RFC3339))
	return 0
}

func runDelete(ctx context.Context, s storage.Storage, args []string, log *slog.Logger) int {
	fs := flag.NewFlagSet("delete", flag.ExitOnError)
	accountID := fs.String("account_id", "", "account UUID (mutually exclusive with --tenant_id / --conversation_id)")
	tenantID := fs.String("tenant_id", "", "tenant UUID (mutually exclusive with --account_id / --conversation_id)")
	conversationID := fs.String("conversation_id", "", "conversation UUID (mutually exclusive with --account_id / --tenant_id)")
	prefix := fs.String("prefix", "mio/attachments/", "key prefix")
	dryRun := fs.Bool("dry-run", false, "report counts without deleting")
	concurrency := fs.Int("concurrency", 8, "max in-flight Stat+Delete operations")
	_ = fs.Parse(args)

	acct := strings.TrimSpace(*accountID)
	tnt := strings.TrimSpace(*tenantID)
	conv := strings.TrimSpace(*conversationID)
	set := 0
	if acct != "" {
		set++
	}
	if tnt != "" {
		set++
	}
	if conv != "" {
		set++
	}
	if set != 1 {
		fmt.Fprintln(os.Stderr, "error: exactly one of --account_id / --tenant_id / --conversation_id is required")
		return 2
	}

	var (
		stats gdpr.Stats
		err   error
	)
	switch {
	case acct != "":
		stats, err = gdpr.DeleteByAccount(ctx, s, *prefix, acct, *dryRun, *concurrency, log)
	case tnt != "":
		stats, err = gdpr.DeleteByTenant(ctx, s, *prefix, tnt, *dryRun, *concurrency, log)
	case conv != "":
		stats, err = gdpr.DeleteByConversation(ctx, s, *prefix, conv, *dryRun, *concurrency, log)
	}
	fmt.Printf("listed=%d matched=%d deleted=%d dry_run=%v\n",
		stats.Listed, stats.Matched, stats.Deleted, *dryRun)
	if err != nil {
		log.Error("delete failed", "err", err)
		return 2
	}
	return 0
}

func runSignedURL(ctx context.Context, s storage.Storage, args []string, log *slog.Logger) int {
	fs := flag.NewFlagSet("signed-url", flag.ExitOnError)
	ttl := fs.Duration("ttl", time.Hour, "URL TTL")
	_ = fs.Parse(args)
	rest := fs.Args()
	if len(rest) != 1 {
		fmt.Fprintln(os.Stderr, "usage: signed-url <key> [--ttl=1h]")
		return 2
	}
	url, err := s.SignedURL(ctx, rest[0], storage.SignOptions{TTL: *ttl})
	if err != nil {
		log.Error("signed-url failed", "err", err)
		return 1
	}
	fmt.Println(url)
	return 0
}

func runSetLifecycle(ctx context.Context, s storage.Storage, args []string, log *slog.Logger) int {
	fs := flag.NewFlagSet("set-lifecycle", flag.ExitOnError)
	ageDays := fs.Int("age-days", 7, "expire objects older than N days")
	prefix := fs.String("prefix", "mio/attachments/", "key prefix")
	_ = fs.Parse(args)

	rules := lifecycle.DefaultRules(*prefix, *ageDays)
	if err := s.SetLifecycle(ctx, rules); err != nil {
		log.Error("set-lifecycle failed", "err", err)
		return 1
	}
	log.Info("set-lifecycle: applied", "prefix", *prefix, "age_days", *ageDays)
	return 0
}
