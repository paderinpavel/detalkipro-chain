package txpool

import (
	"errors"
	"sync"

	"detalki-chain/internal/models"
)

type TxPool struct {
	mu        sync.Mutex
	txs       []models.Transaction
	byOrderID map[string]bool
}

func NewTxPool() *TxPool {
	return &TxPool{
		txs:       make([]models.Transaction, 0),
		byOrderID: make(map[string]bool),
	}
}

func (p *TxPool) Add(tx models.Transaction) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if tx.OrderID == "" {
		return errors.New("order_id обязателен")
	}
	if p.byOrderID[tx.OrderID] {
		return errors.New("дубликат order_id: " + tx.OrderID)
	}
	p.txs = append(p.txs, tx)
	p.byOrderID[tx.OrderID] = true
	return nil
}

func (p *TxPool) TakeN(n int) []models.Transaction {
	p.mu.Lock()
	defer p.mu.Unlock()
	take := n
	if take > len(p.txs) {
		take = len(p.txs)
	}
	taken := make([]models.Transaction, take)
	copy(taken, p.txs[:take])
	p.txs = p.txs[take:]
	for _, t := range taken {
		delete(p.byOrderID, t.OrderID)
	}
	return taken
}

func (p *TxPool) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.txs)
}

func (p *TxPool) All() []models.Transaction {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]models.Transaction, len(p.txs))
	copy(out, p.txs)
	return out
}
