package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"detalki-chain/internal/api"
	"detalki-chain/internal/blockchain"
	"detalki-chain/internal/config"
	"detalki-chain/internal/storage"
	"detalki-chain/internal/txpool"
)

func main() {
	fmt.Println()
	fmt.Println("=== DetalkiPro Chain (PoC) ===")
	fmt.Println("Proof-of-Commerce: сложность падает при реальных заказах (OrderID)")
	fmt.Println()

	cfg := config.Load()

	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		log.Fatalf("Не удалось создать директорию данных: %v", err)
	}

	st := storage.NewJsonStorage(cfg.DataDir + "/chain.json")
	pool := txpool.NewTxPool()

	bc := blockchain.NewBlockchain(&blockchain.Config{
		Difficulty:    cfg.Difficulty,
		MiningReward:  cfg.MiningReward,
		GenesisSupply: cfg.GenesisSupply,
		MaxTxPerBlock: cfg.MaxTxPerBlock,
		DataDir:       cfg.DataDir,
	}, st, pool)

	srv := api.NewServer(bc, cfg)

	mux := http.NewServeMux()
	srv.Routes(mux)

	addr := ":" + cfg.Port
	log.Printf("DetalkiPro Chain сервер запущен на %s", addr)
	log.Printf("Сложность: %d, награда за блок: %d", cfg.Difficulty, cfg.MiningReward)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Получен сигнал завершения — остановка сервера")
		os.Exit(0)
	}()

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Ошибка запуска HTTP сервера: %v", err)
	}
}
