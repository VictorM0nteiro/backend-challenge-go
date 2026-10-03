package httpapi

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/VictorM0nteiro/backend-challenge-go/internal/adapters/postgres"
	"github.com/VictorM0nteiro/backend-challenge-go/internal/domain"
)

func TestStatusFor(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"entrada invalida", fmt.Errorf("%w: x", errInvalidRequest), 400, "invalid_request"},
		{"valor invalido do dominio", domain.ErrInvalidAmount, 400, "invalid_request"},
		{"operacao invalida do dominio", domain.ErrInvalidWagerTransaction, 400, "invalid_request"},
		{"sem cabecalho de provedor", errUnauthenticated, 401, "unauthenticated"},
		{"carteira inexistente", postgres.ErrWalletNotFound, 404, "not_found"},
		{"operacao de outro provedor", errNotFound, 404, "not_found"},
		{"chave reutilizada com corpo diferente", postgres.ErrIdempotencyKeyReuse, 422, "idempotency_key_reused"},
		{"chave ainda em processamento", postgres.ErrRequestInFlight, 409, "request_in_flight"},
		{"carteira ja existe", postgres.ErrWalletAlreadyExists, 409, "conflict"},
		{"versao concorrente", postgres.ErrWalletVersionConflict, 409, "concurrent_update"},
		{"prazo do banco esgotado", fmt.Errorf("acquire: %w", context.DeadlineExceeded), 503, "temporarily_unavailable"},
		{"erro desconhecido", errors.New("boom"), 500, "internal"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, code := statusFor(tc.err)
			if status != tc.status || code != tc.code {
				t.Fatalf("statusFor = %d %s, want %d %s", status, code, tc.status, tc.code)
			}
		})
	}
}
