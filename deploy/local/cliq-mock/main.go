// Command cliq-mock is a dev test-double for Zoho Cliq's REST + OAuth endpoints,
// letting the local stack exercise the gateway's outbound leg with no real Zoho
// org. It accepts the OAuth refresh, hands back a static token, and 204s the bot
// send endpoint — logging every hit so a dev can see the outbound leg land.
//
// ponytail: dev test-double, not a faithful Zoho emulator. Add request-body
// assertions only if a test actually needs them.
package main

import (
	"log"
	"net/http"
	"os"
)

func main() {
	addr := ":8080"
	if v := os.Getenv("CLIQ_MOCK_ADDR"); v != "" {
		addr = v
	}

	mux := http.NewServeMux()

	// OAuth token refresh — gateway posts grant_type=refresh_token here
	// (token.go refreshLocked). Shape matches the fields token.go parses.
	mux.HandleFunc("POST /oauth/v2/token", func(w http.ResponseWriter, _ *http.Request) {
		log.Printf("cliq-mock: oauth token refresh -> 200")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"mock-token","expires_in":3600}`))
	})

	// Bot send endpoint — the real Cliq channelsbyname route returns 204 No Content.
	mux.HandleFunc("POST /api/v2/channelsbyname/{name}/message", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("cliq-mock: outbound send -> 204  channel=%q bot=%q",
			r.PathValue("name"), r.URL.Query().Get("bot_unique_name"))
		w.WriteHeader(http.StatusNoContent)
	})

	// Catch-all so unexpected calls are visible, not silent 404s a dev misreads.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("cliq-mock: UNHANDLED %s %s -> 404", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	})

	log.Printf("cliq-mock listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("cliq-mock: %v", err)
	}
}
