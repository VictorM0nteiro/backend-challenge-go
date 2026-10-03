# Arquitetura e decisões

Este documento registra o que foi decidido, por quê, e o que **não** foi feito. O escopo
foi reduzido para caber no prazo; a seção 10 lista cada lacuna de forma explícita.

## 1. Visão geral

```
cmd/api                  main: carrega .env e roda o grafo Fx
internal/
  domain/                Money, Wallet, WalletLedgerEntry, WagerTransaction, regras
  app/                   Fingerprint: hash canônico do conteúdo de uma operação
  config/                leitura e validação das variáveis de ambiente
  composition/           módulos Fx e ciclo de vida
  adapters/
    postgres/            pool, repositórios, WagerProcessor (a transação financeira)
    httpapi/             rotas, DTOs, autenticação, mapeamento de erro
    sqs/                 consumidor, contrato da mensagem, classificação de erro
  testutil/              containers de teste (Postgres, Keycloak, LocalStack)
migrations/              schema versionado
deploy/                  realm do Keycloak e script das filas
```

`internal/domain` não importa Fx, HTTP, SQS nem pgx. Os testes dele rodam sem Docker.

**Desvio conhecido:** o caso de uso (`WagerProcessor`) mora no adapter Postgres, e não em
uma camada `app` com portas. HTTP e SQS compartilham esse mesmo processador, então a
garantia é uma só, mas a regra de orquestração está acoplada ao pgx. Extrair uma porta é o
próximo passo de arquitetura.

## 2. Dinheiro

- `Money` guarda `int64` em unidades menores (centavos) e o código da moeda. Não existe
  `float` em nenhuma etapa: parsing, cálculo, serialização ou persistência.
- Entrada externa passa por `ParseExternalAmount`: string decimal com no máximo duas casas,
  sem sinal. `"25"`, `"25.0"` e `"25.00"` são o mesmo valor; `"25.001"` e negativos são
  rejeitados. No JSON o valor é sempre string (`"amount": "25.00"`), porque um número JSON
  passaria por ponto flutuante no cliente.
- `Add`, `Sub` e `Negate` checam overflow antes de acontecer e devolvem erro.
- Operar moedas diferentes devolve `ErrCurrencyMismatch`.
- No banco: `BIGINT` (`*_minor`) mais `CHAR(3)`. Escala fixa de 2 casas — moedas com outra
  escala (JPY, BHD) não são suportadas.

## 3. Transação SQL e locks

Biblioteca: `pgx/v5` com SQL explícito, sem ORM.

Toda escrita financeira acontece dentro de **uma** transação, aberta por
`WagerProcessor.Process`:

1. reivindica a linha da inbox (só quando a origem é SQS);
2. reivindica a chave de idempotência (`INSERT ... ON CONFLICT DO NOTHING`);
3. `SELECT ... FOR UPDATE` na carteira;
4. o domínio decide (débito, crédito, rejeição);
5. grava carteira, lançamento do ledger, operação e a resposta armazenada na chave;
6. `COMMIT`.

Qualquer erro desfaz tudo, inclusive as reivindicações dos passos 1 e 2.

- **Lock pessimista** (`FOR UPDATE`) é o mecanismo que impede lost update e saldo negativo.
  O `UPDATE ... WHERE version = $anterior` é uma segunda barreira, que não deveria disparar.
- Uma operação trava **uma** carteira, então não há ordem de locks a respeitar e não há
  deadlock entre operações. Carteiras diferentes não se bloqueiam.
- Os repositórios recebem a `pgx.Tx` de quem chama; nenhum deles abre transação por conta
  própria no caminho financeiro.
- O banco impõe as invariantes mesmo com bug na aplicação: `CHECK (balance_minor >= 0)`,
  `UNIQUE (wallet_id, transaction_id)` no ledger, `UNIQUE (provider_id,
  external_transaction_id)`, índice único parcial que permite no máximo uma reversão
  `PROCESSED` por operação, e triggers que recusam `UPDATE`, `DELETE` e `TRUNCATE` no ledger.

## 4. Idempotência

- Tabela `idempotency_keys`, chave primária `(scope, endpoint, key)`. `scope` é o provedor.
- Guarda `request_hash`, `state` (`in_flight` / `completed`), `status_code` e
  `response_body`.
- **Hash:** SHA-256 em hexadecimal do JSON de `app.WagerBody` (`providerId`,
  `externalTransactionId`, `playerId`, `walletId`, `roundId`, `gameId`, `kind`, `money`,
  `referenceExternalTransactionId`). A ordem dos campos é fixa pelo tipo, sem espaços, e o
  valor é normalizado por `Money`. A chave de idempotência e os metadados de transporte
  ficam de fora. HTTP e SQS usam a mesma função, e um teste compara os dois hashes.

| Situação | Resultado |
|---|---|
| Chave nova | processa, grava a resposta, devolve |
| Mesma chave, mesmo hash | devolve a resposta gravada, `idempotentReplay: true` |
| Mesma chave, hash diferente | 422 `idempotency_key_reused` |
| Chave `in_flight` | 409 `request_in_flight` |

- A reivindicação acontece **dentro** da transação. Uma segunda requisição com a mesma
  chave bloqueia no índice até a primeira terminar e então cai no replay. Por isso
  `in_flight` quase nunca é visível; o ramo existe e é testado com uma linha semeada.
- A resposta é gravada como `TEXT`, não `JSONB`: o `JSONB` reordena chaves, e o replay deve
  devolver o conteúdo original.
- O replay devolve o saldo observado no processamento original.
- **Rejeição de negócio é gravada e reproduzida.** Saldo insuficiente gera uma operação
  `REJECTED` com `failureCode`, auditável. Entrada inválida faz rollback e não consome a chave.

## 5. Regras das operações e reversões

| Tipo | Efeito |
|---|---|
| `BET` | débito; sem saldo vira `REJECTED` / `insufficient_funds` |
| `WIN` | crédito |
| `LOSS` | valor zero, sem lançamento e sem mudar a versão da carteira |
| `REFUND` | desfaz um `BET` (crédito) |
| `ROLLBACK` | desfaz `BET` (crédito), `WIN` ou `REFUND` (débito) |
| `OPENING` | interno; recusado se vier por HTTP ou SQS |

Uma reversão exige referência do mesmo provedor, jogador, carteira, rodada e moeda, em
estado `PROCESSED`, com o mesmo valor. A checagem de "já revertida" acontece **depois** do
lock da carteira, então duas reversões simultâneas da mesma aposta são serializadas; o
índice único parcial é a garantia final. Reversão que deixaria o saldo negativo é rejeitada
com `reversal_insufficient_funds`, distinta de `insufficient_funds`.

**Referências pendentes:** o estado `PENDING_REFERENCE` e suas transições existem no
domínio, mas não há worker que o resolva. Uma reversão cuja referência ainda não chegou é
rejeitada na hora com `reference_not_found`. Isso é uma simplificação em relação ao enunciado.

## 6. Abertura de carteira

`POST /wallets` com saldo positivo grava, na mesma transação: a carteira (versão 1), uma
operação `OPENING` em `PROCESSED`, o lançamento de crédito e dois eventos na outbox. Saldo
zero grava só a carteira. Jogador e moeda repetidos devolvem 409 e nada fica para trás.

## 7. Inbox, SQS e outbox

**Consumidor**
- Uma mensagem por vez, para preservar a ordem dentro do grupo.
- `MessageGroupId` recomendado: `walletId` (ordem por carteira, paralelismo entre carteiras).
  `MessageDeduplicationId` recomendado: `messageId` do envelope. A deduplicação da SQS dura
  5 minutos; a garantia durável é a inbox.
- **Inbox:** uma linha por `(consumidor, messageId)` com o hash, gravada na mesma transação
  da operação. Reentrega com o mesmo corpo vira replay. Mesmo `messageId` com corpo
  diferente é erro permanente.
- A chave usada é `data.idempotencyKey`, no **mesmo espaço** do HTTP. A mesma operação
  enviada pelos dois canais é aplicada uma vez (há teste).
- A mensagem só é apagada depois do commit.

| Resultado | Ação |
|---|---|
| processada ou rejeitada pelo negócio | apaga |
| erro permanente (mensagem inválida, chave ou `messageId` reutilizados, carteira inexistente) | não apaga; o redrive move para a DLQ |
| erro transitório | adia com backoff 2 s, 4 s, 8 s… até 60 s, via visibility timeout |

- Visibility timeout: 30 s. Long polling: 10 s. `maxReceiveCount`: 3.
- No `SIGTERM` o consumidor para de buscar. Se uma mensagem estava em processamento, a
  transação é desfeita e a visibilidade é zerada para outra instância assumir.

**Outbox — parcial.** A tabela existe e a abertura de carteira grava seus eventos no mesmo
commit. Faltam: (1) os eventos das operações de provedor (`BET`, `WIN` etc.) ainda **não**
são gravados; (2) não existe publisher. Nenhum evento é publicado antes do commit porque
nenhum evento é publicado.

## 8. Autenticação e autorização

- **IdP:** Keycloak, `client_credentials`. Escolhido por ser o recomendado no enunciado e
  por permitir provisionar tudo com um arquivo de realm.
- **Validação:** `go-oidc` verifica assinatura (JWKS), `iss` e `exp`. Só `RS256` é aceito,
  o que barra `alg: none` e tokens HMAC. O `aud` não é verificado, porque o Keycloak emite
  `account` nesse fluxo.
- **Identidade:** o claim `azp` é o `providerId`. Ele vai para o contexto da requisição; os
  handlers nunca leem identidade de header ou corpo.
- **Permissões:** um client por provedor. O serviço interno é o client que tem a realm role
  `wallet-internal`. Rotas de carteira exigem a role; rotas de operação a recusam.
- **Isolamento:** corpo com `providerId` diferente do token, ou consulta a operação de outro
  provedor, respondem 404 — igual a recurso inexistente, para não revelar existência. A
  chave de idempotência é escopada por provedor, então um replay não cruza provedores.
- `AUTH_ISSUER` e `AUTH_JWKS_URL` são separados porque, no Compose, o token traz
  `localhost:8081` no `iss` e o app alcança o Keycloak por `keycloak:8080`.

Limitações: não há revogação antes do `exp` (5 min); todo client do realm sem a role
interna é tratado como provedor; a entrada por SQS **não** é autenticada por mensagem — o
`providerId` vem do corpo, e a confiança é em quem pode escrever na fila.

## 9. Fx e shutdown

Módulos: `config`, `persistence`, `httpapi`, `sqs`. Construtores via `fx.Provide`,
inicialização via `fx.Invoke`, recursos via `fx.Lifecycle`.

- Configuração inválida, banco inacessível ou porta ocupada falham na montagem do grafo.
- O JWKS e a fila são acessados sob demanda; o app sobe mesmo sem IdP ou SQS no ar, e
  `/health/ready` informa `database` e `queue`. O IdP não entra no readiness.
- Encerramento na ordem inversa da construção: servidor HTTP (`Shutdown`, drena o que está
  em andamento) → consumidor SQS (cancela e espera o loop terminar) → pool. A ordem vem do
  grafo de dependências, não de uma lista manual.
- Testes: `fx.ValidateApp` e um teste que inicia, atende uma requisição, encerra e verifica
  que o pool foi fechado.

Limitação: o Fx corta o encerramento em 15 s (`fx.StopTimeout` não foi ligado a
`SHUTDOWN_TIMEOUT`); valores maiores não têm efeito.

## 10. Trabalho não concluído

| Item do enunciado | Situação |
|---|---|
| Publisher da outbox, disputa entre publishers, recuperação | não feito |
| Eventos de outbox para operações de provedor | não feito (só a abertura grava) |
| Worker de `PENDING_REFERENCE` com backoff | não feito; reversão sem referência é rejeitada |
| `POST /wallets/:id/reconciliation` | não feito |
| `GET /wallets/:id/ledger` paginado por cursor | não feito |
| `GET /providers/:providerId/wagering/transactions/:externalTransactionId` | não feito |
| Prova com três instâncias, `kill -9`, indisponibilidade de Postgres/SQS | não feita; o desenho não depende de memória local (lock e idempotência estão no banco), mas isso não foi demonstrado |
| Métricas, tracing | não feito |
| Log de acesso HTTP | não feito; só erros 500 e o consumidor geram log |
| Idempotência de `POST /wallets` | só pela unicidade `(player_id, currency)`; repetição devolve 409, não replay |

Outras exigências do enunciado não atendidas:

| Item do enunciado | Situação |
|---|---|
| Logs em JSON com `correlationId`, `transactionId`, `walletId`, `providerId` (§12) | não feito; os logs saem em texto, e só o consumidor carrega `messageId` |
| Envelope de evento com `eventId`, `correlationId`, `version` etc., e payload completo de `WalletBalanceChanged` (§11) | não feito; os dois eventos da abertura têm payload reduzido |
| Destino dos eventos de saída provisionado (§11) | não feito |
| Controle de acesso à fila por credenciais e políticas do broker (§2) | não feito; o LocalStack aceita qualquer credencial |
| `OPENING` sem provedor, ID externo, chave e hash (§6.3) | desvio: a linha usa o provedor `internal`, o identificador `opening:{walletId}` e chave e hash vazios. A unicidade `(provider_id, external_transaction_id)` impede crédito inicial duplicado |
| Estado `FAILED` para falha permanente de infraestrutura (§6.3) | existe no domínio, nunca é gravado |
| Moeda validada contra a lista ISO 4217 (§6.1) | só o formato (três letras maiúsculas) é validado |
| 50 envios paralelos da mesma aposta (§13.1) | o teste usa 10 |
| Carteiras distintas em paralelo (§13.3) | sem teste; decorre do lock por linha |
| Interrupção entre commit e remoção da mensagem (§13.5) | sem teste de interrupção; a reentrega da mesma mensagem é testada |
| Reinício da aplicação preservando idempotência (§13.8) | sem teste; as chaves estão no banco |
| Conferência de saldo contra a soma do ledger ao fim dos testes (§13) | não feita |
| Credencial expirada contra o IdP real (§13) | testada só com token assinado no próprio teste |
| Instruções para múltiplas instâncias e simulação de falhas (§15) | não escritas, porque os cenários não foram executados |

Defeitos conhecidos:

- **Jogador não é conferido contra a carteira.** O processador trava a carteira pelo
  `walletId` e não verifica se o `playerId` da requisição é o dono dela. Um provedor
  autenticado pode movimentar uma carteira informando outro jogador, e o valor errado fica
  gravado na operação. É a lacuna de integridade mais séria desta entrega.
- **`LOSS` não confere a moeda da carteira**, embora o enunciado exija (§7).
- **Mesma operação com outra chave.** Reenviar o mesmo `(providerId,
  externalTransactionId)` com uma chave de idempotência diferente não reaplica o efeito (a
  constraint `UNIQUE` barra e a transação é desfeita), mas a resposta é um 500 genérico em
  vez de um conflito. Por SQS, o mesmo caso é tratado como transitório e só chega à DLQ
  após as tentativas.
- **`WIN` com referência** não é validado contra a aposta de origem.
- O ledger não tem chave estrangeira para `wager_transactions`.
- A mensagem de `idempotency_key_reused` expõe o prefixo interno `postgres:`.
- Readiness consulta as dependências a cada chamada, sem cache.

## 11. Situação por critério de avaliação

Autoavaliação contra a tabela do enunciado (§14). Sem estimativa de pontos: o objetivo é
apontar onde está a evidência e onde ela falta.

| Critério (pontos) | Feito | Não feito |
|---|---|---|
| **Integridade financeira (20)** | `Money` em `int64`, sem float, com overflow tratado; saldo não negativo por `CHECK`; ledger imutável por trigger; lançamento validado (`balanceAfter = balanceBefore ± valor`); reversões com valor igual, mesma rodada e no máximo uma bem-sucedida; códigos distintos para falta de saldo em aposta e em reversão | reconciliação; conferência jogador × carteira; moeda em `LOSS`; validação da referência de `WIN` |
| **Concorrência (20)** | `SELECT ... FOR UPDATE` por carteira, sem lock global; versão como segunda barreira; teste obrigatório 100 / 80 / 80 repetido 100 vezes com `-race`; estado todo no banco, nada em memória do processo | prova com três processos independentes; teste de carteiras distintas em paralelo |
| **Idempotência (15)** | chave persistida na mesma transação do efeito; conflito por hash (422); replay com o saldo original; hash canônico igual entre HTTP e SQS; rejeição de negócio também reproduzida; teste com 10 requisições simultâneas e teste cruzando HTTP e SQS | resposta de conflito para a mesma operação com outra chave (hoje 500); teste com 50 envios; teste de reinício |
| **Mensageria e recuperação (15)** | inbox na mesma transação, com hash; remoção só após o commit; backoff por visibility timeout; DLQ com redrive, testada; liberação da mensagem no `SIGTERM` | publisher da outbox; eventos das operações de provedor; envelope de evento; worker de `PENDING_REFERENCE`; testes de interrupção |
| **Modelagem e arquitetura (10)** | entidades com estado privado, construtores com validação, criação separada de reidratação, erros com `errors.Is`; domínio sem dependência de infraestrutura; Fx com módulos, `Provide`, `Invoke` e `Lifecycle`; provedor definido pelo token, rotas internas por role | caso de uso dentro do adapter Postgres, sem porta; controle de acesso à fila |
| **Testes (10)** | Postgres, Keycloak e LocalStack reais; isolamento entre provedores em envio e consulta; token ausente, inválido, de outro emissor e com algoritmo errado; composição e ciclo de vida do Fx | interrupção e reinício; três instâncias; publishers concorrentes; reversão antes da referência com resolução posterior |
| **Observabilidade (5)** | `/health/live`; `/health/ready` com banco e fila; logs do consumidor com `messageId` | logs em JSON com identificadores de correlação; métricas; log de acesso HTTP |
| **Documentação (5)** | `docker compose up --build` a partir de checkout limpo; IdP, filas e migrations provisionados; `.env.example`; decisões e lacunas neste arquivo | instruções de múltiplas instâncias e simulação de falhas |

Condições eliminatórias (§14):

| Condição | Situação |
|---|---|
| Autenticação efetiva nos endpoints de negócio | atendida |
| Acesso não autorizado a operações ou transações | atendida entre provedores; ver o defeito "jogador não é conferido contra a carteira" |
| Cálculo monetário em ponto flutuante | não ocorre |
| Saldo negativo por concorrência | impedido por lock e por `CHECK` |
| Movimentação duplicada | impedida por chave, inbox e constraints |
| Idempotência só em memória | não ocorre; está no banco |
| Dependência de uma única instância | o desenho não depende, mas **não foi demonstrado** com várias instâncias |
| Publicação anterior ao commit | não ocorre; nada é publicado |
| Ledger auditável | atendida |
| Infraestrutura toda em mocks | não ocorre |

## 12. Pontos de falha da implantação

- **Postgres é ponto único de falha.** Sem réplica nem failover. É também o único
  coordenador: lock, idempotência e inbox dependem dele. Não há consenso distribuído.
- **Pool:** uma transação por operação segura uma conexão durante o lock; carteira muito
  disputada enfileira no banco. `DB_ACQUIRE_TIMEOUT` transforma espera em erro rápido.
- **SQS / LocalStack fora:** o HTTP continua funcionando; o consumidor tenta de novo a cada
  segundo e o readiness passa a 503.
- **Keycloak fora:** tokens já validados continuam aceitos enquanto as chaves estiverem em
  cache; chaves novas não são obtidas.
