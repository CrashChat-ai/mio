package rest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

func (s *Server) handleTailMessages(w http.ResponseWriter, r *http.Request) {
	accountID := strings.TrimSpace(r.URL.Query().Get("account_id"))
	if accountID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "account_id_required"})
		return
	}
	conversationID := strings.TrimSpace(r.URL.Query().Get("conversation_id"))
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming_not_supported"})
		return
	}
	stream, err := s.admin.TailMessages(r.Context(), accountID, conversationID)
	if err != nil {
		s.writeAdminError(w, err)
		return
	}
	defer stream.Close() //nolint:errcheck

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	for stream.Receive() {
		payload, err := json.Marshal(tailMessageToJSON(stream.Msg()))
		if err != nil {
			s.logger.Warn("mio-web tail marshal failed", "error", err)
			continue
		}
		if _, err := fmt.Fprintf(w, "event: message\ndata: %s\n\n", payload); err != nil {
			return
		}
		flusher.Flush()
	}
	if err := stream.Err(); err != nil && !errors.Is(err, context.Canceled) {
		s.logger.Warn("mio-web tail stream failed", "error", err)
		writeSSEError(w, flusher, "tail_failed")
	}
}

func writeSSEError(w http.ResponseWriter, flusher http.Flusher, code string) {
	payload, _ := json.Marshal(map[string]string{"error": code})
	_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", payload)
	flusher.Flush()
}
