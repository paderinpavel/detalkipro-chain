package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"detalki-chain/internal/stratumserver"
)

func (s *Server) handleMinerStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		jsonResp(w, 405, map[string]string{"error": "только POST"})
		return
	}
	var req struct {
		StratumPort int    `json:"stratum_port"`
		MinerAddr   string `json:"miner_addr"`
	}
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&req)
		r.Body.Close()
	}
	if req.StratumPort == 0 {
		req.StratumPort = 3333
	}
	if req.MinerAddr == "" {
		if err := s.ensureWallet(); err != nil {
			handleError(w, fmt.Errorf("укажите miner_addr или создайте кошелёк через /wallet/new"))
			return
		}
		req.MinerAddr = s.wallet.Address
	}
	s.stratum = stratumserver.New(s.bc, req.StratumPort, req.MinerAddr)
	if err := s.stratum.Start(); err != nil {
		handleError(w, err)
		return
	}
	jsonResp(w, 200, map[string]any{
		"status":      "Stratum-прокси запущен",
		"proxy_port":  req.StratumPort,
		"miner_addr":  req.MinerAddr,
		"instruction": "На Apollo: pool=stratum+tcp://IP_КОМПЬЮТЕРА:" + fmt.Sprintf("%d", req.StratumPort) + ", worker=dtk.miner, pass=x",
	})
}

func (s *Server) handleMinerStop(w http.ResponseWriter, r *http.Request) {
	if s.stratum == nil {
		handleError(w, fmt.Errorf("майнер не запущен"))
		return
	}
	s.stratum.Stop()
	s.stratum = nil
	jsonResp(w, 200, map[string]string{"status": "Stratum-прокси остановлен"})
}

func (s *Server) handleMinerStatus(w http.ResponseWriter, r *http.Request) {
	if s.stratum == nil {
		jsonResp(w, 200, map[string]any{"running": false})
		return
	}
	jsonResp(w, 200, map[string]any{"running": true})
}

func (s *Server) handlePoCStats(w http.ResponseWriter, r *http.Request) {
	miner := ""
	if s.wallet != nil {
		miner = s.wallet.Address
	}
	tmpl := s.bc.GetBlockTemplate(miner)
	if tmpl == nil {
		jsonResp(w, 200, map[string]any{
			"base_difficulty":  s.bc.Cfg.Difficulty,
			"max_discount":     2,
			"tx_per_discount":  s.bc.Cfg.MaxTxPerBlock,
			"explanation":      "За каждые N транзакций с OrderID сложность снижается на 1 ноль (макс -2). Пустые блоки майнить сложнее.",
		})
		return
	}
	commerceCount := s.bc.CountCommerceTx(tmpl.Block.Transactions)
	diff := s.bc.EffectiveDifficulty(commerceCount)
	jsonResp(w, 200, map[string]any{
		"base_difficulty":      s.bc.Cfg.Difficulty,
		"effective_difficulty": diff,
		"commerce_tx_count":    commerceCount,
		"discount_applied":     s.bc.Cfg.Difficulty - diff,
		"max_discount":         2,
		"tx_per_discount":      s.bc.Cfg.MaxTxPerBlock,
		"explanation":          "Чем больше реальных заказов (OrderID) в блоке — тем ниже сложность и быстрее майнится блок.",
	})
}
