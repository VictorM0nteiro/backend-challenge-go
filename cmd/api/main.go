package main

import (
	"errors"
	"io/fs"
	"log"

	"github.com/VictorM0nteiro/backend-challenge-go/internal/composition"
	"github.com/joho/godotenv"
	"go.uber.org/fx"
)

func main() {
	if err := godotenv.Load(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Fatalf("load .env: %v", err)
	}
	fx.New(composition.Options()...).Run()
}

// Explicação

// - Run bloqueia até receber SIGINT/SIGTERM e então executa os OnStop na ordem certa. Não precisei escrever signal.NotifyContext como no wallet-go, porque o Fx já faz isso.
// - Como ainda não há servidor, o processo sobe, conecta no banco e fica esperando sinal. Isso é esperado nesta etapa.
