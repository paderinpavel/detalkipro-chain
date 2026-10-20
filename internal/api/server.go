package api

import (
	"log"
	"net/http"

	"detalki-chain/internal/blockchain"
	"detalki-chain/internal/config"
	"detalki-chain/internal/models"
	"detalki-chain/internal/stratumserver"
)

type Server struct {
	bc      *blockchain.Blockchain
	cfg     *config.Config
	wallet  *models.Wallet
	stratum *stratumserver.Server
}

func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.APIKey != "" {
			key := r.Header.Get("X-API-Key")
			if key != s.cfg.APIKey {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
		}
		next(w, r)
	}
}

func (s *Server) recoverMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("PANIC: %v", rec)
				http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
			}
		}()
		next(w, r)
	}
}

func (s *Server) Routes(mux *http.ServeMux) {
	wrap := func(h http.HandlerFunc) http.HandlerFunc {
		return s.recoverMiddleware(h)
	}

	mux.HandleFunc("/health", wrap(s.handleHealth))
	mux.HandleFunc("/status", wrap(s.handleStatus))
	mux.HandleFunc("/balance", wrap(s.handleBalance))
	mux.HandleFunc("/wallet/new", wrap(s.handleWalletNew))
	mux.HandleFunc("/wallet", wrap(s.handleWalletShow))
	mux.HandleFunc("/send", wrap(s.authMiddleware(s.handleSend)))
	mux.HandleFunc("/send-signed", wrap(s.authMiddleware(s.handleSendSigned)))
	mux.HandleFunc("/distribute", wrap(s.authMiddleware(s.handleDistribute)))
	mux.HandleFunc("/mine", wrap(s.handleMine))
	mux.HandleFunc("/chain", wrap(s.handleChain))
	mux.HandleFunc("/validate", wrap(s.handleValidate))
	mux.HandleFunc("/mempool", wrap(s.handleMempool))
	mux.HandleFunc("/tx/status", wrap(s.handleTxStatus))

	// Miner & PoC routes
	mux.HandleFunc("/miner/start", wrap(s.authMiddleware(s.handleMinerStart)))
	mux.HandleFunc("/miner/stop", wrap(s.authMiddleware(s.handleMinerStop)))
	mux.HandleFunc("/miner/status", wrap(s.handleMinerStatus))
	mux.HandleFunc("/poc/stats", wrap(s.handlePoCStats))
}

func NewServer(bc *blockchain.Blockchain, cfg *config.Config) *Server {
	return &Server{bc: bc, cfg: cfg}
}
