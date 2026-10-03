package config

import (
	"errors"
	"fmt"
	"strconv"
	"time"
)

// ErrMissingDatabaseURL is returned when DATABASE_URL is absent or empty.
var ErrMissingDatabaseURL = errors.New("config: DATABASE_URL is required")

// Config holds everything the process needs from its environment.
type Config struct {
	DatabaseURL    string
	HTTPAddr       string
	PoolMaxConns   int32
	AcquireTimeout time.Duration
}

// Lookup has the same shape as os.LookupEnv. Taking it as a parameter keeps
// Load free of global state: tests pass a map instead of the real environment.
type Lookup func(key string) (string, bool)

// Load reads and validates the configuration. Invalid values fail here, at
// startup, instead of surfacing later as a confusing runtime error.
func Load(lookup Lookup) (Config, error) {
	dsn, ok := lookup("DATABASE_URL")
	if !ok || dsn == "" {
		return Config{}, ErrMissingDatabaseURL
	}

	cfg := Config{
		DatabaseURL:    dsn,
		HTTPAddr:       ":8080",
		PoolMaxConns:   10,
		AcquireTimeout: 5 * time.Second,
	}

	if raw, ok := lookup("HTTP_ADDR"); ok && raw != "" {
		cfg.HTTPAddr = raw
	}

	if raw, ok := lookup("DB_MAX_CONNS"); ok {
		n, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || n <= 0 {
			return Config{}, fmt.Errorf("config: DB_MAX_CONNS must be a positive integer, got %q", raw)
		}
		cfg.PoolMaxConns = int32(n)
	}

	if raw, ok := lookup("DB_ACQUIRE_TIMEOUT"); ok {
		d, err := time.ParseDuration(raw)
		if err != nil || d <= 0 {
			return Config{}, fmt.Errorf("config: DB_ACQUIRE_TIMEOUT must be a positive duration, got %q", raw)
		}
		cfg.AcquireTimeout = d
	}

	return cfg, nil
}


// Explicação

// - Load recebe o Lookup em vez de chamar os.LookupEnv direto. Assim o teste controla o ambiente sem tocar nas variáveis reais do processo. É o mesmo motivo de injetar dependências por construtor.
// - Os padrões ficam em um único lugar. Quem lê Load sabe o valor efetivo de cada variável.
// - DATABASE_URL é obrigatória e sem ela o processo nem sobe. DB_MAX_CONNS e DB_ACQUIRE_TIMEOUT são validadas: 0 ou texto inválido vira erro no startup, não um pool com comportamento estranho.
// - int32 no ParseInt com bitSize 32 evita overflow silencioso ao converter para o tipo que o pgxpool espera.
