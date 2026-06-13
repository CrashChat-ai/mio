package rest

import "net/http"

func (s *Server) handleChannelTypes(w http.ResponseWriter, r *http.Request) {
	channels, err := s.admin.ListChannelTypes(r.Context())
	if err != nil {
		s.writeAdminError(w, err)
		return
	}
	out := make([]channelTypeJSON, 0, len(channels))
	for _, channel := range channels {
		out = append(out, channelTypeToJSON(channel))
	}
	writeJSON(w, http.StatusOK, map[string]any{"channelTypes": out})
}
