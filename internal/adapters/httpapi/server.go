package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/VictorM0nteiro/backend-challenge-go/internal/adapters/postgres"
	"github.com/google/uuid"
)

const (
	headerProvider       = "X-provider-ID"
	headerIdempotencyKey = "Idempotency-Key"
	maxIdempotencyKeyLen = 255
	maxBodyBytes         = 64 << 10
)

// Server exposes the README §9 contracts. It holds no business rules: each
// handler decodes the request, checks its shape, and hands the work to the
// persistence layer, which owns the transactions and the locks.
type Server struct {
	pool      *postgres.Pool
	wallets   *postgres.WalletRepository
	wagers    *postgres.WagerReader
	processor *postgres.WagerProcessor
}

func NewServer(pool *postgres.Pool, wallets *postgres.WalletRepository, wagers *postgres.WagerReader, processor *postgres.WagerProcessor) *Server {
	return &Server{pool: pool, wallets: wallets, wagers: wagers, processor: processor}
}

// Handler returns the routes. Go 1.22 patterns put the method in the route, so
// a wrong method gets 405 from the mux and never reaches a handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /wallets", handle(s.openWallet))
	mux.HandleFunc("GET /wallets/{walletId}", handle(s.getWallet))
	mux.HandleFunc("POST /wagering/transactions", handle(s.submitWager))
	mux.HandleFunc("GET /wagering/transactions/{transactionId}", handle(s.getWager))
	mux.HandleFunc("GET /health/live", handle(s.live))
	mux.HandleFunc("GET /health/ready", handle(s.ready))
	return mux
}

// handler returns an error instead of writing it, so the mapping from errors
// to status codes lives in one place (statusFor).
type handler func(w http.ResponseWriter, r *http.Request) error

func handle(h handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			writeError(w, err)
		}
	}
}

// providerFrom returns the caller's provider. Until the auth step, the header
// is trusted. The auth step replaces this function and nothing else.
func providerFrom(r *http.Request) (string, error) {
	provider := r.Header.Get(headerProvider)
	if provider == "" {
		return "", errUnauthenticated
	}
	return provider, nil
}

// decodeJSON reads one JSON object into dst. Unknown fields and oversized
// bodies are refused, so a typo in a field name fails loudly instead of being
// ignored.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("%w: malformed JSON: %v", errInvalidRequest, err)
	}
	return nil
}

// pathUUID parses a path parameter as a UUID.
func pathUUID(r *http.Request, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue(name))
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: %s must be a UUID", errInvalidRequest, name)
	}
	return id, nil
}
