package httpapi

import (
	"context"
	"net/http"
	"time"
)

// live answers while the process can serve requests at all.
func (s *Server) live(w http.ResponseWriter, _ *http.Request) error {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	return nil
}

// ready answers 503 when the database cannot be reached. The check is bounded,
// so a hung database makes readiness fail fast instead of hanging the probe.
func (s *Server) ready(w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := s.pool.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "database": "down"})
		return nil
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "database": "up"})
	return nil
}

// Explicação

// - live não toca no banco. Se o processo travar, o liveness falha; se só o banco cair, ele continua vivo e o readiness responde 503.
// Essa separação é o que impede o orquestrador de reiniciar o processo por causa de um banco fora do ar.
