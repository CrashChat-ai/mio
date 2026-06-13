package auth

import (
	"net/http"
	"strings"
)

type CORSConfig struct {
	AllowedOrigins []string
	AllowedMethods []string
	AllowedHeaders []string
}

func ParseCORSOrigins(spec string) []string {
	var out []string
	for _, raw := range strings.Split(spec, ",") {
		origin := strings.TrimRight(strings.TrimSpace(raw), "/")
		if origin != "" {
			out = append(out, origin)
		}
	}
	return out
}

// CORS returns an allowlist credentialed-CORS middleware. With no allowed
// origins it returns next unchanged so the single-origin path emits no headers.
// Cross-origin credentialed requests also require SameSite=None;Secure cookies,
// which is out of the default single-origin scope.
func CORS(cfg CORSConfig, next http.Handler) http.Handler {
	if len(cfg.AllowedOrigins) == 0 {
		return next
	}
	allowed := make(map[string]struct{}, len(cfg.AllowedOrigins))
	for _, o := range cfg.AllowedOrigins {
		allowed[o] = struct{}{}
	}
	methods := strings.Join(orDefault(cfg.AllowedMethods, []string{
		http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions,
	}), ", ")
	headers := strings.Join(orDefault(cfg.AllowedHeaders, []string{"Content-Type", "Authorization"}), ", ")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			if _, ok := allowed[origin]; ok {
				h := w.Header()
				h.Add("Vary", "Origin")
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Access-Control-Allow-Credentials", "true")
				if r.Method == http.MethodOptions {
					h.Set("Access-Control-Allow-Methods", methods)
					h.Set("Access-Control-Allow-Headers", headers)
				}
			}
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func orDefault(v, fallback []string) []string {
	if len(v) == 0 {
		return fallback
	}
	return v
}
