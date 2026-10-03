package postgres

import "errors"

var (
	ErrWalletNotFound        = errors.New("postgres: wallet not found")
	ErrWalletVersionConflict = errors.New("postgres: wallet version conflict")
	ErrDuplicateLedgerEntry  = errors.New("postgres: duplicate ledger entry for this wallet and transaction")
)

// Explicação
// Três erros, cada um documentando quem de verdade garante a invariante — isso é proposital. Se um entrevistador perguntar 
// "o que impede duas atualizações perdidas?", a resposta certa não é "o código verifica a versão", é "o SELECT FOR UPDATE trava a linha; 
// a checagem de versão é só uma rede de segurança que nunca deveria disparar". Documentar isso no próprio erro evita que você (ou outra pessoa) 
// conclua errado, meses depois, sobre qual mecanismo é o principal.