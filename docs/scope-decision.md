# Decisão de escopo — prazo de 3 dias

> Documento de referência. Registra o que será entregue, o que fica parcial, o que fica
> fora, e por quê — para consulta durante a implementação e para citar no `ARCHITECTURE.md`
> final.

---

## 1. Contexto que motiva o corte

O `README.md` descreve um desafio de nível pleno/sênior: Uber Fx obrigatório, OIDC/Keycloak
obrigatório, SQS/LocalStack obrigatório, inbox/outbox com DLQ, máquina de estados assíncrona,
prova com três processos independentes.

A vaga a que esse desafio se refere é **júnior** (faixa R$ 2.500–5.000, "experiência
profissional anterior não é obrigatória"), e lista explicitamente **Uber Fx** e
**concorrência em Go (goroutines/channels)** em "Diferenciais — não obrigatórios". A vaga
valoriza nomeadamente: "reconhecer quando pedir ajuda antes que um risco cresça" e
"investigar problemas... aplicar feedback".

O prazo de entrega é de **3 dias, fixado pela empresa, não negociável**.

Conclusão: completar o README inteiro no padrão que ele descreve não é viável no prazo.
A estratégia é entregar um subconjunto bem feito e documentar o resto com julgamento — que é
exatamente a competência que a própria vaga diz procurar.

---

## 2. Camada 0 — nunca pular (condições eliminatórias do §14)

Custam pouco tempo e anulam a nota inteira se ausentes:

- Dinheiro nunca passa por `float32`/`float64`, em nenhuma etapa.
- Nenhuma escrita financeira fora de uma transação SQL explícita.
- Idempotência persistida (nunca só em memória).
- Pelo menos um teste de integração real contra Postgres e um contra LocalStack — a
  suíte não pode ser 100% mockada.
- Autenticação real (validação de JWT do Keycloak), mesmo sem políticas de autorização
  ricas. Ausência total de auth é eliminatória; auth simples não é.
- Publicação de evento só depois do commit que a originou.
- Ledger auditável e append-only.
- Não depender de uma única instância para funcionar corretamente (mesmo que a prova
  formal com 3 processos fique parcial — ver seção 3).

---

## 3. Escopo por seção do README

| § | Item | Decisão |
|---|---|---|
| 2 | Autenticação OIDC/Keycloak | **Sim**, mínimo: valida JWT real (assinatura + claims), sem RBAC rico |
| 3 | Cenários de falha: duplicata, concorrência na mesma carteira | **Sim** |
| 3 | Cenários de falha: `kill -9`, indisponibilidade Postgres/SQS | **Documentado**, sem prova extensiva |
| 4 | Go + Fx + Postgres/pgx + migrations + testing | **Sim** |
| 4 | SQS via LocalStack | **Sim**, mínimo (um consumidor funcionando) |
| 5 | As 8 garantias obrigatórias (sem float, idempotência persistida, invariantes no banco, ledger append-only, carteiras paralelas, sem lost update) | **Sim** — é o núcleo |
| 6.1 | `Money` (decimal, moeda, overflow, serialização) | **Sim** |
| 6.2 | `Wallet` (saldo + versão armazenados, débito/crédito) | **Sim** |
| 6.3 | `WagerTransaction` + estados `PENDING/PROCESSED/REJECTED/FAILED` | **Sim** |
| 6.3 | `PENDING_REFERENCE` com worker de backoff sobrevivendo a restart | **Parcial**: uma tentativa de resolução, documentado o que falta |
| 6.4 | `WalletLedgerEntry` imutável | **Sim** |
| 6.5 | Inbox / Outbox | **Sim**, versão simples (um publisher, sem disputa) |
| 7 | Regras dos 5 tipos (`BET/WIN/LOSS/REFUND/ROLLBACK`) + proteção contra reversão duplicada | **Sim** — núcleo de domínio |
| 8 | Concorrência numa carteira (teste do saldo 100 / duas apostas de 80) | **Sim** |
| 8 | Prova com 3 processos independentes | **Parcial**: `docker compose --scale`, teste básico cross-instância |
| 9 | Contratos HTTP (criar carteira, leituras, enviar operação, reconciliação, health checks) | **Sim**, todos |
| 10 | Consumidor SQS (fila, redrive, dedup, shutdown) | **Sim**, mínimo funcional; tuning de redrive documentado |
| 11 | Outbox com múltiplos publishers disputando + recuperação de interrupção nos dois pontos | **Não** — gap consciente, documentado |
| 12 | Logs estruturados + health checks | **Sim** |
| 12 | Métricas ricas, tracing, dashboards | **Não** (diferencial explicitamente opcional) |
| 13 | Testes unitários (`Money`, `Wallet`, transições, regras dos 5 tipos) | **Sim** |
| 13 | Integração real (Postgres + IdP + LocalStack, pelo menos smoke) | **Sim** — evita o eliminatório de "tudo mockado" |
| 13 | Cenários de concorrência #1, #2, #3 (50x mesma aposta, disputa 80/80, carteiras paralelas) | **Sim** |
| 13 | Cenários #5, #7, #8 (interrupção de consumidor, `REFUND` antes da referência, restart) | **Parcial**, se der tempo no fim |
| 13 | Cenário #6 (dois publishers disputando outbox) | **Não** |
| 15 | `ARCHITECTURE.md` nomeando o que não foi feito | **Sim** — parte da entrega, não opcional |

---

## 4. Projeção de cobertura por critério de avaliação (§14)

| Critério | Pontos | Cobre | Fica de fora | Expectativa |
|---|---:|---|---|---|
| Integridade financeira | 20 | `Money`, `Wallet` versionada, `WalletLedgerEntry`, as 8 garantias, regras dos 5 tipos, reconciliação | Resolução robusta de reversão sem referência (parcial) | Forte |
| Concorrência | 20 | `Wallet` versionada, teste do §8, cenários #1/#2/#3 | Prova formal com 3 processos (parcial) | Forte, com ressalva |
| Idempotência | 15 | Persistência + hash, conflito de corpo, replay, dedup de inbox | — | Forte |
| Mensageria e recuperação | 15 | Consumidor SQS mínimo, inbox/outbox simples | Outbox multi-publisher, DLQ/redrive fino, `PENDING_REFERENCE` completo, cenário #6 | Fraco/parcial — maior corte |
| Modelagem e arquitetura | 10 | Fx básico, domínio encapsulado, auth real mínima | Políticas de autorização mais ricas | Boa |
| Testes | 10 | Unitários, integração real (smoke), cenários #1/#2/#3 | Cenários #4/#5/#6 | Boa, com buracos pontuais |
| Observabilidade | 5 | Logs estruturados, health checks | Métricas, tracing | Parcial |
| Documentação | 5 | `ARCHITECTURE.md` com limitações nomeadas, README reproduzível | — | Forte |

**Estimativa ilustrativa (não é garantia):** ~75-80 de 100, assumindo que a Camada 0
(seção 2) permanece intacta. Qualquer item eliminatório que falhar anula essa conta.

---

## 5. Ordem de execução recomendada

1. `Money` → `Wallet` (saldo + versão) → débito/crédito com lock pessimista
2. Teste obrigatório do §8 (saldo 100, duas apostas de 80 simultâneas)
3. Idempotência (chave + hash + replay), reaproveitando o padrão já validado no projeto
   `wallet-go` (claim/execute/complete na mesma transação)
4. Contratos HTTP dos §9 (carteira, operação, leituras, reconciliação, health checks)
5. Autenticação OIDC mínima
6. Consumidor SQS mínimo + inbox/outbox simples
7. Testes dos cenários #1/#2/#3; se houver tempo, #5/#7/#8
8. `ARCHITECTURE.md` e `README.md` da solução, nomeando tudo que ficou na Camada "Não"
   ou "Parcial"

---

## 6. Revisão desta decisão

Se o tempo permitir mais do que o previsto, a ordem de prioridade para "subir de camada"
é: `PENDING_REFERENCE` completo → cenário #6 → outbox multi-publisher → métricas. Não
inverter essa ordem — ela segue o peso dos critérios do §14.
