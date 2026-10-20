package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"detalki-chain/internal/models"
	"detalki-chain/internal/wallet"
)



func (s *Server) ensureWallet() error {
	if s.wallet != nil {
		return nil
	}
	w, err := wallet.Load(s.cfg.DataDir + "/wallet.json")
	if err != nil {
		return fmt.Errorf("кошелёк не найден — создайте через /wallet/new")
	}
	s.wallet = w
	return nil
}

func jsonResp(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(data)
}

func handleError(w http.ResponseWriter, err error) {
	log.Println("ERROR:", err)
	jsonResp(w, 400, map[string]string{"error": err.Error()})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	jsonResp(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	jsonResp(w, 200, s.bc.Status())
}

func (s *Server) handleBalance(w http.ResponseWriter, r *http.Request) {
	addr := r.URL.Query().Get("addr")
	if addr == "" {
		handleError(w, fmt.Errorf("нужен параметр addr"))
		return
	}
	jsonResp(w, 200, map[string]any{
		"address": addr,
		"balance": s.bc.Balance(addr),
		"nonce":   s.bc.Nonce(addr),
	})
}

func (s *Server) handleWalletNew(w http.ResponseWriter, r *http.Request) {
	wlt, err := wallet.New()
	if err != nil {
		handleError(w, err)
		return
	}
	if err := wallet.Save(wlt, s.cfg.DataDir + "/wallet.json"); err != nil {
		handleError(w, err)
		return
	}
	s.wallet = wlt
	jsonResp(w, 200, map[string]string{
		"address": wlt.Address,
		"message": "кошелёк создан и сохранён в wallet.json",
	})
}

func (s *Server) handleWalletShow(w http.ResponseWriter, r *http.Request) {
	if err := s.ensureWallet(); err != nil {
		handleError(w, err)
		return
	}
	jsonResp(w, 200, map[string]string{"address": s.wallet.Address})
}

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		jsonResp(w, 405, map[string]string{"error": "только POST"})
		return
	}
	var tx models.Transaction
	if err := json.NewDecoder(r.Body).Decode(&tx); err != nil {
		handleError(w, fmt.Errorf("неверный JSON"))
		return
	}
	defer r.Body.Close()
	if tx.Fee == 0 {
		tx.Fee = 1
	}
	if err := s.bc.AddTx(tx); err != nil {
		handleError(w, err)
		return
	}
	jsonResp(w, 200, map[string]string{
		"status": "принята в мемпул",
		"order":  tx.OrderID,
		"from":   tx.From,
		"to":     tx.To,
		"amount": fmt.Sprintf("%d", tx.Amount),
	})
}

func (s *Server) handleSendSigned(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		jsonResp(w, 405, map[string]string{"error": "только POST"})
		return
	}
	if err := s.ensureWallet(); err != nil {
		handleError(w, err)
		return
	}
	var req struct {
		To      string `json:"to"`
		Amount  int64  `json:"amount"`
		OrderID string `json:"order_id"`
		Fee     int64  `json:"fee"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handleError(w, fmt.Errorf("неверный JSON"))
		return
	}
	defer r.Body.Close()
	if req.Fee == 0 {
		req.Fee = 1
	}
	nonce := s.bc.Nonce(s.wallet.Address)
	tx := models.Transaction{
		To:      req.To,
		Amount:  req.Amount,
		Fee:     req.Fee,
		Nonce:   nonce,
		OrderID: req.OrderID,
	}
	wallet.SignTx(s.wallet, &tx)
	if err := s.bc.AddTx(tx); err != nil {
		handleError(w, err)
		return
	}
	jsonResp(w, 200, map[string]string{
		"status": "подписана и принята",
		"tx_id":  tx.Sig[:16],
		"order":  tx.OrderID,
		"from":   tx.From,
		"to":     tx.To,
	})
}

func (s *Server) handleMine(w http.ResponseWriter, r *http.Request) {
	miner := r.URL.Query().Get("miner")
	if miner == "" {
		if s.wallet != nil {
			miner = s.wallet.Address
		} else {
			handleError(w, fmt.Errorf("укажите ?miner=ADDR или создайте кошелёк"))
			return
		}
	}
	block, err := s.bc.MineBlock(miner)
	if err != nil {
		handleError(w, err)
		return
	}
	jsonResp(w, 200, block)
}

func (s *Server) handleChain(w http.ResponseWriter, r *http.Request) {
	jsonResp(w, 200, s.bc.Chain())
}

func (s *Server) handleValidate(w http.ResponseWriter, r *http.Request) {
	ok := s.bc.ValidateChain()
	jsonResp(w, 200, map[string]any{"valid": ok, "blocks": len(s.bc.Chain())})
}

func (s *Server) handleMempool(w http.ResponseWriter, r *http.Request) {
	jsonResp(w, 200, s.bc.Mempool())
}

func (s *Server) handleTxStatus(w http.ResponseWriter, r *http.Request) {
	orderID := r.URL.Query().Get("order_id")
	if orderID == "" {
		handleError(w, fmt.Errorf("нужен параметр order_id"))
		return
	}
	confirmed, blockNum, tx := s.bc.TxStatus(orderID)
	if tx == nil {
		jsonResp(w, 404, map[string]string{"status": "не найдена"})
		return
	}
	if confirmed {
		jsonResp(w, 200, map[string]any{"confirmed": true, "block": blockNum, "tx": tx})
	} else {
		jsonResp(w, 200, map[string]any{"confirmed": false, "status": "в мемпуле"})
	}
}

func (s *Server) handleDistribute(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		jsonResp(w, 405, map[string]string{"error": "только POST"})
		return
	}
	var req struct {
		To     string `json:"to"`
		Amount int64  `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handleError(w, fmt.Errorf("неверный JSON"))
		return
	}
	defer r.Body.Close()
	if err := s.bc.AddGenesisDistribution(req.To, req.Amount); err != nil {
		handleError(w, err)
		return
	}
	jsonResp(w, 200, map[string]string{
		"status": "в мемпуле",
		"to":     req.To,
		"amount": fmt.Sprintf("%d", req.Amount),
	})
}
