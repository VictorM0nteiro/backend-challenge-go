package config

import (
	"errors"
	"testing"
	"time"
)

func envOf(m map[string]string) Lookup {
	return func(key string) (string, bool) {
		v, ok := m[key]
		return v, ok
	}
}

func TestLoad(t *testing.T) {
	t.Run("sem_database_url_falha", func(t *testing.T) {
		_, err := Load(envOf(map[string]string{}))
		if !errors.Is(err, ErrMissingDatabaseURL) {
			t.Fatalf("err = %v, want ErrMissingDatabaseURL", err)
		}
	})

	t.Run("aplica_padroes", func(t *testing.T) {
		cfg, err := Load(envOf(map[string]string{"DATABASE_URL": "postgres://x"}))
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.PoolMaxConns != 10 || cfg.AcquireTimeout != 5*time.Second || cfg.HTTPAddr != ":8080" {
			t.Fatalf("defaults = %+v", cfg)
		}
	})

	t.Run("rejeita_pool_zero", func(t *testing.T) {
		_, err := Load(envOf(map[string]string{"DATABASE_URL": "postgres://x", "DB_MAX_CONNS": "0"}))
		if err == nil {
			t.Fatal("DB_MAX_CONNS=0 must be rejected")
		}
	})

	t.Run("aplica_shutdown_padrao_e_rejeita_invalido", func(t *testing.T) {
		cfg, err := Load(envOf(map[string]string{"DATABASE_URL": "postgres://x"}))
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.ShutdownTimeout != 10*time.Second {
			t.Fatalf("ShutdownTimeout = %s, want 10s", cfg.ShutdownTimeout)
		}

		_, err = Load(envOf(map[string]string{"DATABASE_URL": "postgres://x", "SHUTDOWN_TIMEOUT": "-1s"}))
		if err == nil {
			t.Fatal("negative SHUTDOWN_TIMEOUT must be rejected")
		}
	})

	t.Run("rejeita_timeout_invalido", func(t *testing.T) {
		_, err := Load(envOf(map[string]string{"DATABASE_URL": "postgres://x", "DB_ACQUIRE_TIMEOUT": "cinco"}))
		if err == nil {
			t.Fatal("unparseable DB_ACQUIRE_TIMEOUT must be rejected")
		}
	})
}

// Explicação

// - Cada subteste cobre uma decisão: ausência, padrão, e os dois tipos de valor inválido. Se alguém remover a validação de <= 0, o teste de pool zero quebra.
// - envOf é um adaptador de map para Lookup. Não há estado global, então os testes rodam em paralelo sem interferir.
