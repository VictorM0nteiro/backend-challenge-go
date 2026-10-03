package domain

import "errors"

var (
	ErrInvalidAmount     = errors.New("domain: invalid amount")
	ErrInvalidCurrency   = errors.New("domain: invalid currency code")
	ErrCurrencyMismatch  = errors.New("domain: currency mismatch")
	ErrMoneyOverflow     = errors.New("domain: money operantion overflows int64")
	ErrInsufficientFunds = errors.New("domain: insufficient funds")
)

// cinco erros de domínio, como valores sentinela comparáveis com errors.Is —
// exatamente a convenção do wallet-go, e também uma exigência explícita do README
// (§6, "erros de domínio devem ser classificáveis por tipo ou errors.Is/errors.As").
// Se te perguntarem na entrevista "por que não usar panic?": o README é explícito —
//  panic não deve representar rejeição de negócio. Rejeição é um caminho esperado do sistema
//  (saldo insuficiente é rotina, não uma falha de programação), então é um valor de retorno,
//   não uma interrupção de controle de fluxo.
