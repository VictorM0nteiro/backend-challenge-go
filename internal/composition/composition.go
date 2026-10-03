package composition

import (
	"context"
	"os"

	"github.com/VictorM0nteiro/backend-challenge-go/internal/adapters/postgres"
	"github.com/VictorM0nteiro/backend-challenge-go/internal/config"
	"go.uber.org/fx"
)

// Options is the whole application graph. main runs it and the test validates
// it, so both read exactly the same wiring.
func Options() []fx.Option {
	return []fx.Option{
		ConfigModule,
		PersistenceModule,
		fx.Invoke(requirePool),
	}
}

// ConfigModule provides the configuration read from the environment.
var ConfigModule = fx.Module("config",
	fx.Provide(func() (config.Config, error) {
		return config.Load(os.LookupEnv)
	}),
)

// PersistenceModule provides the connection pool and the adapters built on it.
var PersistenceModule = fx.Module("persistence",
      fx.Provide(
              newPool,
              postgres.NewWalletRepository,
              postgres.NewWagerProcessor,
      ),
)

// newPool opens the pool and registers its shutdown with the lifecycle.
// Fx stops hooks in the reverse order they were appended. The pool is built
// before anything that depends on it, so its hook runs after every component
// that uses it has stopped. That ordering comes from the dependency graph,
// not from a manual list.
func newPool(lc fx.Lifecycle, cfg config.Config) (*postgres.Pool, error) {
      pool, err := postgres.NewPool(context.Background(), postgres.PoolConfig{
              DSN:            cfg.DatabaseURL,
              MaxConns:       cfg.PoolMaxConns,
              AcquireTimeout: cfg.AcquireTimeout,
      })
      if err != nil {
              return nil, err
      }

      lc.Append(fx.Hook{
              OnStop: func(context.Context) error {
                      pool.Close()
                      return nil
              },
      })
      return pool, nil
}

func requirePool(*postgres.Pool) {}