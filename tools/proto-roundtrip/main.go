// Package main implements the mio.v1 proto round-trip test.
//
// Test protocol:
//  1. Go marshals a fully-populated Message{} to wire bytes.
//  2. Wire bytes are piped to the Python half (tools/proto-roundtrip/roundtrip.py)
//     via stdin/stdout.
//  3. Python decodes, re-encodes, and writes back to stdout.
//  4. Go decodes the Python-re-encoded bytes and asserts field-by-field equality.
//
// Additionally exercises:
//   - Unknown-field tolerance: a hand-crafted message with unknown fields 17 and 18
//     is decoded by both Go and Python — neither must error.
//   - Subject-token validator: tokens containing dots are rejected; valid tokens pass.
//
// Lives in the root module (github.com/crashchat-ai/mio).
// No separate go.mod — the root go.mod pins google.golang.org/protobuf.
package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"time"

	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// tokenRE matches valid NATS subject tokens: only [a-zA-Z0-9_-].
// A dot in any token would split the subject hierarchy unexpectedly.
var tokenRE = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// ValidateSubjectToken rejects tokens that contain illegal characters.
// This is the seed for the SDK's publish-time validator (P2).
// Rejects rather than sanitises — callers must normalise first.
func ValidateSubjectToken(token string) error {
	if !tokenRE.MatchString(token) {
		return fmt.Errorf("subject token %q contains illegal characters; only [a-zA-Z0-9_-] allowed (dots split NATS subjects)", token)
	}
	return nil
}

// repoRoot walks up from this file's directory to find the repo root (go.mod).
func repoRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		log.Fatal("cannot determine source file path")
	}
	// tools/proto-roundtrip/main.go → ../../
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func buildMessage() *miov1.Message {
	return &miov1.Message{
		Id:                     "018fbe3e-7c00-7a00-8000-000000000001",
		SchemaVersion:          1,
		TenantId:               "tenant-alpha",
		AccountId:              "018fbe3e-7c00-7a00-8000-000000000002",
		ChannelType:            "zoho_cliq",
		ConversationId:         "018fbe3e-7c00-7a00-8000-000000000003",
		ConversationExternalId: "cliq-chat-abc123",
		ConversationKind:       miov1.ConversationKind_CONVERSATION_KIND_DM,
		ParentConversationId:   "018fbe3e-7c00-7a00-8000-000000000004",
		SourceMessageId:        "cliq-msg-xyz789",
		ThreadRootMessageId:    "018fbe3e-7c00-7a00-8000-000000000005",
		Sender: &miov1.Sender{
			ExternalId:  "cliq-user-001",
			DisplayName: "Alice",
			PeerKind:    miov1.PeerKind_PEER_KIND_DIRECT,
			IsBot:       false,
		},
		Text: "Hello from MIO round-trip test",
		Attachments: []*miov1.Attachment{
			{
				Kind:     miov1.Attachment_KIND_IMAGE,
				Url:      "https://cdn.example.com/img.png",
				Mime:     "image/png",
				Bytes:    204800,
				Filename: "img.png",
			},
		},
		ReceivedAt: timestamppb.New(time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)),
		Attributes: map[string]string{
			"zoho_cliq_workspace": "alpha-workspace",
			"zoho_cliq_bot_id":    "bot-001",
		},
		Relation: &miov1.MessageRelation{
			Kind:              miov1.MessageRelation_KIND_REACTION,
			TargetMessageId:   "018fbe3e-7c00-7a00-8000-000000000006",
			TargetExternalId:  "cliq-msg-target-987",
			ReactionEmoji:     "🚀",
		},
	}
}

// assertMessageEqual compares two messages field by field and returns any mismatches.
func assertMessageEqual(original, decoded *miov1.Message) error {
	if !proto.Equal(original, decoded) {
		return fmt.Errorf("messages not equal after round-trip:\n  original: %v\n  decoded:  %v", original, decoded)
	}
	return nil
}

// runPythonHalf pipes raw bytes through the Python script and returns re-encoded bytes.
func runPythonHalf(root string, raw []byte) ([]byte, error) {
	scriptPath := filepath.Join(root, "tools", "proto-roundtrip", "roundtrip.py")
	sdkPyPath := filepath.Join(root, "sdks", "python")

	cmd := exec.Command("uv", "run", "--project", sdkPyPath, scriptPath)
	cmd.Stdin = bytes.NewReader(raw)
	cmd.Stderr = os.Stderr

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("python half failed: %w", err)
	}
	return out, nil
}

// testUnknownFieldTolerance verifies that a message containing unknown fields 17
// and 18 (reserved for future use) is decoded without error by both Go and Python.
//
// We hand-craft the wire bytes by appending varint-encoded unknown fields:
//
//	field 17 (varint wire type 0): tag = (17 << 3) | 0 = 136 → 0x88 0x01
//	field 18 (varint wire type 0): tag = (18 << 3) | 0 = 144 → 0x90 0x01
func testUnknownFieldTolerance(root string, baseRaw []byte) error {
	// Append unknown field 17 (varint, value=42) and field 18 (varint, value=99).
	// Wire format: tag (varint) + value (varint)
	// field 17 tag: (17<<3)|0 = 136 → multi-byte varint: 0x88, 0x01
	// field 18 tag: (18<<3)|0 = 144 → multi-byte varint: 0x90, 0x01
	unknownFields := []byte{
		0x88, 0x01, 0x2a, // field 17, varint, value=42
		0x90, 0x01, 0x63, // field 18, varint, value=99
	}
	withUnknown := append(baseRaw, unknownFields...)

	// Go decode must not error.
	var goMsg miov1.Message
	if err := proto.Unmarshal(withUnknown, &goMsg); err != nil {
		return fmt.Errorf("Go decoder rejected unknown fields 17+18: %w", err)
	}

	// Python decode (via round-trip) must not error.
	pyOut, err := runPythonHalf(root, withUnknown)
	if err != nil {
		return fmt.Errorf("Python decoder rejected unknown fields 17+18: %w", err)
	}

	// Verify Python output is valid proto (Go can decode it back).
	var pyMsg miov1.Message
	if err := proto.Unmarshal(pyOut, &pyMsg); err != nil {
		return fmt.Errorf("Go cannot decode Python's re-encoded bytes (unknown-field test): %w", err)
	}

	return nil
}

// testSubjectTokenValidator runs unit checks on ValidateSubjectToken.
func testSubjectTokenValidator() error {
	valid := []string{"zoho_cliq", "account-123", "conv_abc", "ABC123", "a-b_c"}
	for _, tok := range valid {
		if err := ValidateSubjectToken(tok); err != nil {
			return fmt.Errorf("valid token %q unexpectedly rejected: %w", tok, err)
		}
	}

	// Tokens with dots must be rejected (they would split NATS subject hierarchy).
	invalid := []string{"has.dot", "two..dots", "trail.", ".lead", "a.b.c"}
	for _, tok := range invalid {
		if err := ValidateSubjectToken(tok); err == nil {
			return fmt.Errorf("token with dot %q was not rejected", tok)
		}
	}

	// Other illegal characters.
	alsoInvalid := []string{"has space", "slash/bad", "star*bad", "gt>bad", "hash#bad"}
	for _, tok := range alsoInvalid {
		if err := ValidateSubjectToken(tok); err == nil {
			return fmt.Errorf("token %q with illegal chars was not rejected", tok)
		}
	}

	return nil
}

func main() {
	root := repoRoot()

	// --- Step 1: subject-token validator unit tests ---
	fmt.Print("subject-token validator ... ")
	if err := testSubjectTokenValidator(); err != nil {
		log.Fatalf("FAIL: %v", err)
	}
	fmt.Println("OK")

	// --- Step 2: Go marshal ---
	msg := buildMessage()
	raw, err := proto.Marshal(msg)
	if err != nil {
		log.Fatalf("FAIL: Go marshal: %v", err)
	}
	fmt.Printf("Go marshal ... OK (%d bytes)\n", len(raw))

	// --- Step 3: Go decode (self-check) ---
	var goDecoded miov1.Message
	if err := proto.Unmarshal(raw, &goDecoded); err != nil {
		log.Fatalf("FAIL: Go unmarshal: %v", err)
	}
	if err := assertMessageEqual(msg, &goDecoded); err != nil {
		log.Fatalf("FAIL: Go self-roundtrip: %v", err)
	}
	fmt.Println("Go self-roundtrip ... OK")

	// --- Step 4: Python half ---
	fmt.Print("Python roundtrip ... ")
	pyOut, err := runPythonHalf(root, raw)
	if err != nil {
		log.Fatalf("FAIL: %v", err)
	}
	fmt.Printf("OK (%d bytes back)\n", len(pyOut))

	// --- Step 5: Go decode Python's re-encoded bytes ---
	var pyDecoded miov1.Message
	if err := proto.Unmarshal(pyOut, &pyDecoded); err != nil {
		log.Fatalf("FAIL: Go decode of Python output: %v", err)
	}
	if err := assertMessageEqual(msg, &pyDecoded); err != nil {
		log.Fatalf("FAIL: Go/Python field equality: %v", err)
	}
	fmt.Println("Go/Python field equality ... OK")

	// --- Step 6: unknown-field tolerance (reserved fields 17 + 18) ---
	fmt.Print("Unknown-field tolerance (fields 17+18) ... ")
	if err := testUnknownFieldTolerance(root, raw); err != nil {
		log.Fatalf("FAIL: %v", err)
	}
	fmt.Println("OK")

	fmt.Println("\nOK")
}
