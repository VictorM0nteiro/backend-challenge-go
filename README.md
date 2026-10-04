# Wallet & Wagering — desafio backend Go

Serviço em Go que processa operações financeiras de provedores de jogos (`BET`, `WIN`,
`LOSS`, `REFUND`, `ROLLBACK`) sobre carteiras de jogadores, por HTTP e por SQS, com
idempotência persistida e ledger append-only.

- Enunciado original: [docs/README-desafio.md](docs/README-desafio.md)
- Decisões, limitações e o que não foi feito: [ARCHITECTURE.md](ARCHITECTURE.md)
- Feito e não feito por critério de avaliação: [ARCHITECTURE.md § 11](ARCHITECTURE.md#11-situação-por-critério-de-avaliação)
- Decisão de escopo para o prazo de 3 dias: [docs/scope-decision.md](docs/scope-decision.md)

> O escopo foi reduzido de forma consciente. O que ficou de fora está listado em
> [ARCHITECTURE.md § Trabalho não concluído](ARCHITECTURE.md#10-trabalho-não-concluído).

## Pré-requisitos

- Docker com Docker Compose
- Go 1.25 (só para rodar os testes ou a aplicação fora do container)

## Subir tudo

```sh
docker compose up --build
```

| Serviço | Endereço | O que é |
|---|---|---|
| `app` | http://localhost:8080 | a API e o consumidor SQS |
| `postgres` | localhost:5432 | banco (`wallet` / `wallet` / `wallet`) |
| `keycloak` | http://localhost:8081 | IdP, realm `wallet` importado na subida (admin `admin` / `admin`) |
| `localstack` | http://localhost:4566 | SQS |
| `migrate` | — | aplica as migrations e termina com código 0 |

Nada precisa ser configurado à mão: o realm do Keycloak, as filas e as migrations são
provisionados na subida. Para zerar o estado: `docker compose down -v`.

## Identidades de teste

Todas usam `client_credentials`. Os segredos são de desenvolvimento e estão versionados em
`deploy/keycloak/wallet-realm.json`.

| Client | Segredo | Papel |
|---|---|---|
| `provider-a` | `provider-a-dev-secret` | provedor |
| `provider-b` | `provider-b-dev-secret` | provedor |
| `wallet-service` | `wallet-service-dev-secret` | serviço interno (role `wallet-internal`) |

Obter um token (bash):

```sh
token() {
  curl -s -X POST http://localhost:8081/realms/wallet/protocol/openid-connect/token \
    -d grant_type=client_credentials -d client_id="$1" -d client_secret="$2" \
    | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4
}
SVC=$(token wallet-service wallet-service-dev-secret)
PA=$(token provider-a provider-a-dev-secret)
```

O token vale 5 minutos.

## Exemplos de chamadas

Abrir uma carteira (serviço interno):

```sh
curl -s -X POST http://localhost:8080/wallets \
  -H "Authorization: Bearer $SVC" -H "Content-Type: application/json" \
  -d '{"playerId":"0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1","initialBalance":{"amount":"1000.00","currency":"BRL"}}'
```

Enviar uma aposta (provedor). Troque `WALLET_ID` pelo `id` devolvido acima:

```sh
curl -s -X POST http://localhost:8080/wagering/transactions \
  -H "Authorization: Bearer $PA" -H "Content-Type: application/json" \
  -H "Idempotency-Key: provider-a:transaction-123" \
  -d '{"providerId":"provider-a","externalTransactionId":"transaction-123","playerId":"0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1","walletId":"WALLET_ID","roundId":"round-987","gameId":"fortune-chimp","kind":"BET","money":{"amount":"25.00","currency":"BRL"}}'
```

Repetir o mesmo comando devolve o mesmo resultado com `"idempotentReplay": true`, sem
segundo débito. Para uma reversão, acrescente `"referenceExternalTransactionId"` ao corpo.

Consultas:

```sh
curl -s http://localhost:8080/wallets/WALLET_ID -H "Authorization: Bearer $SVC"
curl -s http://localhost:8080/wagering/transactions/TRANSACTION_ID -H "Authorization: Bearer $PA"
curl -s http://localhost:8080/health/live
curl -s http://localhost:8080/health/ready
```

Enviar a mesma operação pela fila:

```sh
docker compose exec localstack awslocal sqs send-message \
  --queue-url http://localhost:4566/000000000000/wager-transactions.fifo \
  --message-group-id WALLET_ID --message-deduplication-id msg-1 \
  --message-body '{"messageId":"msg-1","type":"WagerTransactionRequested","occurredAt":"2026-09-08T12:00:00.000Z","data":{"providerId":"provider-a","externalTransactionId":"transaction-124","idempotencyKey":"provider-a:transaction-124","playerId":"0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1","walletId":"WALLET_ID","roundId":"round-987","gameId":"fortune-chimp","kind":"BET","money":{"amount":"10.00","currency":"BRL"}}}'
```

## Contrato HTTP

| Rota | Quem pode chamar |
|---|---|
| `POST /wallets` | serviço interno |
| `GET /wallets/{walletId}` | serviço interno |
| `POST /wagering/transactions` (exige `Idempotency-Key`) | provedor |
| `GET /wagering/transactions/{transactionId}` | provedor dono da operação |
| `GET /health/live`, `GET /health/ready` | público |

Respostas e como distingui-las:

| Situação | Status | Corpo |
|---|---|---|
| Operação processada | 201 | `status: PROCESSED`, `balance`, `idempotentReplay` |
| Rejeição de negócio (ex.: saldo insuficiente) | 422 | `status: REJECTED`, `failureCode`, **sem** envelope `error` |
| Mesma chave, corpo diferente | 422 | `error.code: idempotency_key_reused` |
| Chave ainda em processamento | 409 | `error.code: request_in_flight` |
| Mesma operação (provedor + `externalTransactionId`) com outra chave | 409 | `error.code: duplicate_operation` |
| Carteira já existe para jogador e moeda | 409 | `error.code: conflict` |
| Entrada inválida | 400 | `error.code: invalid_request` |
| Token ausente, inválido ou expirado | 401 | `error.code: unauthenticated` |
| Token válido sem permissão para a rota | 403 | `error.code: forbidden` |
| Recurso inexistente ou de outro provedor | 404 | `error.code: not_found` |
| Banco não respondeu no prazo | 503 | `error.code: temporarily_unavailable` |
| Dependência fora (`/health/ready`) | 503 | `checks: {database, queue}` |

Códigos de rejeição (`failureCode`): `insufficient_funds`, `reversal_insufficient_funds`,
`reference_not_found`, `reference_not_processed`, `reference_mismatch`,
`reference_amount_mismatch`, `duplicate_reversal`.

## Variáveis de ambiente

No Compose elas já estão definidas em `docker-compose.yml`. Para rodar fora do container,
copie `.env.example` para `.env` (o binário lê o `.env` do diretório atual; uma variável já
definida no ambiente tem precedência).

| Variável | Obrigatória | Padrão | Significado |
|---|---|---|---|
| `DATABASE_URL` | sim | — | DSN do Postgres |
| `AUTH_ISSUER` | sim | — | `iss` exigido nos tokens |
| `AUTH_JWKS_URL` | sim | — | de onde as chaves públicas são baixadas |
| `SQS_QUEUE_URL` | sim | — | fila FIFO consumida |
| `SQS_ENDPOINT` | não | vazio (AWS) | endpoint do SQS; use o do LocalStack localmente |
| `AWS_REGION` | não | `us-east-1` | região |
| `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` | sim para o SDK | — | no LocalStack, qualquer valor |
| `HTTP_ADDR` | não | `:8080` | endereço do servidor |
| `DB_MAX_CONNS` | não | `10` | tamanho do pool |
| `DB_ACQUIRE_TIMEOUT` | não | `5s` | prazo de cada operação no banco |
| `SHUTDOWN_TIMEOUT` | não | `10s` | prazo para drenar requisições no encerramento |

## Migrations

No Compose, o serviço `migrate` aplica tudo antes do `app` subir. Manualmente:

```sh
# aplicar
docker compose run --rm migrate -path /migrations \
  -database "postgres://wallet:wallet@postgres:5432/wallet?sslmode=disable" up
# reverter a última
docker compose run --rm migrate -path /migrations \
  -database "postgres://wallet:wallet@postgres:5432/wallet?sslmode=disable" down 1
```

## Filas

`deploy/localstack/init/ready.d/10-queues.sh` cria, a cada subida do LocalStack,
`wager-transactions.fifo` e `wager-transactions-dlq.fifo`, com redrive após 3 recebimentos.

## Testes

Os testes de integração usam containers reais (Postgres, Keycloak e LocalStack) via
testcontainers. Só é preciso ter o Docker em execução; as imagens são baixadas na primeira
vez. Não há build tags.

```sh
go vet ./...
go test ./... -p 1
go test -race ./... -p 1
go test ./internal/domain/...     # só domínio: sem Docker, menos de 1 s
```

`-p 1` roda um pacote por vez. No Windows com Docker Desktop, pacotes em paralelo às vezes
falham ao conectar no Docker. `-race` exige um compilador C de 64 bits (`gcc`).

O que cada pacote prova:

| Pacote | Prova |
|---|---|
| `internal/domain` | `Money`, carteira, máquina de estados, regras dos cinco tipos, abertura |
| `internal/adapters/postgres` | constraints e triggers, ledger imutável, disputa de saldo (100 / duas apostas de 80), idempotência com 10 requisições simultâneas, reversões |
| `internal/adapters/httpapi` | contratos HTTP com tokens reais do Keycloak, isolamento entre provedores |
| `internal/adapters/sqs` | consumo real no LocalStack, reentrega, mesma operação por HTTP e SQS, DLQ |
| `internal/composition` | grafo Fx válido; início, atendimento e encerramento liberando o pool |

## Teste de carga

Com a pilha de pé:

```sh
go run ./cmd/loadtest -scenario many -wallets 50 -workers 32 -duration 60s
go run ./cmd/loadtest -scenario single -workers 32 -duration 60s
```

Metodologia, ambiente e resultados em [docs/loadtest.md](docs/loadtest.md). Os relatórios
JSON ficam em `docs/bench/`.

## Rodar fora do container

```sh
docker compose up -d postgres migrate keycloak localstack
cp .env.example .env    # ajuste DATABASE_URL para wallet:wallet@localhost:5432/wallet
go run ./cmd/api
```

`Ctrl+C` encerra na ordem: servidor HTTP, consumidor SQS, pool do banco.
