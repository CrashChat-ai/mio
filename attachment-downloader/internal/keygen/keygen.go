// Package keygen builds the canonical content-addressable object key for
// persisted attachments.
//
//	{prefix}{channel_type}/yyyy=YYYY/mm=MM/dd=DD/{sha256[:2]}/{sha256}{ext}
//
// Date partitioning uses the inbound message's received_at, not "now" — so
// the prefix-delete used by retention sweeps is chronologically meaningful.
package keygen

import (
	"mime"
	"path"
	"strings"
	"time"
)

// Build returns the canonical key. prefix already contains the trailing slash
// (e.g. "mio/attachments/").
func Build(prefix, channelType, sha256hex, contentType, filename string, receivedAt time.Time) string {
	if receivedAt.IsZero() {
		receivedAt = time.Now().UTC()
	} else {
		receivedAt = receivedAt.UTC()
	}
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	if channelType == "" {
		channelType = "unknown"
	}
	shortSha := sha256hex
	if len(shortSha) >= 2 {
		shortSha = sha256hex[:2]
	}
	ext := pickExt(contentType, filename)
	return prefix +
		channelType + "/" +
		"yyyy=" + receivedAt.Format("2006") + "/" +
		"mm=" + receivedAt.Format("01") + "/" +
		"dd=" + receivedAt.Format("02") + "/" +
		shortSha + "/" +
		sha256hex + ext
}

// pickExt returns a leading "." extension or "" if it can't determine one.
// Prefers contentType (authoritative), falls back to filename's extension.
func pickExt(contentType, filename string) string {
	if contentType != "" {
		// strings such as "image/png; charset=utf-8" — strip params first.
		mt := contentType
		if i := strings.IndexByte(mt, ';'); i > 0 {
			mt = strings.TrimSpace(mt[:i])
		}
		if exts, _ := mime.ExtensionsByType(mt); len(exts) > 0 {
			return exts[0]
		}
	}
	if filename != "" {
		if e := path.Ext(filename); e != "" {
			return strings.ToLower(e)
		}
	}
	return ""
}
