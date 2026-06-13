package rest

import "net/http"

func (s *Server) handleStreamHealth(w http.ResponseWriter, r *http.Request) {
	health, err := s.admin.GetStreamHealth(r.Context())
	if err != nil {
		s.writeAdminError(w, err)
		return
	}
	consumers := make([]consumerHealthJSON, 0, len(health.GetConsumers()))
	for _, c := range health.GetConsumers() {
		consumers = append(consumers, consumerHealthToJSON(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"consumers": consumers})
}
