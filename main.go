package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ===== КОНФИГУРАЦИЯ =====

type Config struct {
	Port          string
	DataDir       string
	Difficulty    int
	MiningReward  int64
	GenesisSupply int64
	APIKey        string
	MaxTxPerBlock int
}

func loadConfig() *Config {
	return &Config{
		Port:          envOr("PORT", "8080"),
		DataDir:       envOr("DATA_DIR", "."),
		Difficulty:    intEnvOr("DIFFICULTY", 4),
		MiningReward:  int64EnvOr("MINING_REWARD", 50),
		GenesisSupply: int64EnvOr("GENESIS_SUPPLY", 100000000),
		APIKey:        os.Getenv("API_KEY"),
		MaxTxPerBlock: intEnvOr("MAX_TX_PER_BLOCK", 50),
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
func intEnvOr(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
func int64EnvOr(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}

// ===== ТИПЫ ДАННЫХ =====

type Transaction struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Amount  int64  `json:"amount"`
	Fee     int64  `json:"fee"`
	Nonce   int64  `json:"nonce"`
	OrderID string `json:"order_id"`
	PubKey  string `json:"pubkey"`
	Sig     string `json:"sig"`
}

type Block struct {
	Index        int64         `json:"index"`
	Timestamp    int64         `json:"timestamp"`
	Transactions []Transaction `json:"transactions"`
	PrevHash     string        `json:"prev_hash"`
	Hash         string        `json:"hash"`
	Nonce        int64         `json:"nonce"`
	Miner        string        `json:"miner"`
}

type State struct {
	Chain    []Block          `json:"chain"`
	Balances map[string]int64 `json:"balances"`
	Nonces   map[string]int64 `json:"nonces"`
}

type Wallet struct {
	PrivateKey ed25519.PrivateKey `json:"-"`
	PublicKey  ed25519.PublicKey  `json:"-"`
	Address    string             `json:"address"`
	PrivHex    string             `json:"private_key"`
	PubHex     string             `json:"public_key"`
}

// ===== БЛОКЧЕЙН =====

type Blockchain struct {
	mu       sync.Mutex
	state    State
	cfg      *Config
	chainFile string
	mempool  []Transaction
}

func NewBlockchain(cfg *Config) *Blockchain {
	bc := &Blockchain{
		cfg:       cfg,
		chainFile: cfg.DataDir + "/chain.json",
		mempool:   []Transaction{},
	}
	if !bc.load() {
		log.Println("Цепочка не найдена — создаём генезис...")
		bc.createGenesis()
		bc.save()
	}
	return bc
}

func (bc *Blockchain) createGenesis() {
	genesisAddr := "detalki_genesis_owner_0000000000000000000000000000000000"
	genesisTx := Transaction{
		From:    "GENESIS",
		To:      genesisAddr,
		Amount:  bc.cfg.GenesisSupply,
		PubKey:  "GENESIS",
		Sig:     "GENESIS",
	}
	g := Block{
		Index:        0,
		Timestamp:    time.Now().Unix(),
		Transactions: []Transaction{genesisTx},
		PrevHash:     "0",
		Miner:        "genesis",
	}
	g.Hash = bc.calcHash(g)
	bc.state.Chain = []Block{g}
	bc.state.Balances = map[string]int64{genesisAddr: bc.cfg.GenesisSupply}
	bc.state.Nonces = map[string]int64{}
}

func (bc *Blockchain) calcHash(b Block) string {
	txData, _ := json.Marshal(b.Transactions)
	raw := fmt.Sprintf("%d%d%s%s%d%s", b.Index, b.Timestamp, string(txData), b.PrevHash, b.Nonce, b.Miner)
	h := sha256.New()
	h.Write([]byte(raw))
	return hex.EncodeToString(h.Sum(nil))
}

func (bc *Blockchain) txMessage(tx *Transaction) string {
	return fmt.Sprintf("%s%s%d%d%d%s", tx.From, tx.To, tx.Amount, tx.Fee, tx.Nonce, tx.OrderID)
}

func (bc *Blockchain) verifyTx(tx *Transaction) error {
	if tx.From == "GENESIS" || tx.PubKey == "GENESIS" {
		return nil
	}
	if tx.Amount <= 0 {
		return fmt.Errorf("сумма должна быть > 0")
	}
	if tx.From == "" || tx.To == "" {
		return fmt.Errorf("адреса from и to обязательны")
	}
	pubBytes, err := hex.DecodeString(tx.PubKey)
	if err != nil || len(pubBytes) != ed25519.PublicKeySize {
		return fmt.Errorf("неверный публичный ключ")
	}
	sigBytes, err := hex.DecodeString(tx.Sig)
	if err != nil || len(sigBytes) != ed25519.SignatureSize {
		return fmt.Errorf("неверная подпись")
	}
	msg := bc.txMessage(tx)
	if !ed25519.Verify(pubBytes, []byte(msg), sigBytes) {
		return fmt.Errorf("подпись не валидна")
	}
	balance := bc.state.Balances[tx.From]
	if balance < tx.Amount+tx.Fee {
		return fmt.Errorf("недостаточно средств: баланс %d, нужно %d", balance, tx.Amount+tx.Fee)
	}
	if tx.Nonce != bc.state.Nonces[tx.From] {
		return fmt.Errorf("неверный nonce: ожидается %d, получено %d", bc.state.Nonces[tx.From], tx.Nonce)
	}
	return nil
}

func (bc *Blockchain) addTx(tx Transaction) error {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if err := bc.verifyTx(&tx); err != nil {
		return err
	}
	bc.mempool = append(bc.mempool, tx)
	log.Printf("TX в мемпуле: %s -> %s, %d DTK, order=%s", tx.From[:12], tx.To[:12], tx.Amount, tx.OrderID)
	return nil
}

func (bc *Blockchain) mineBlock(miner string) (*Block, error) {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	last := bc.state.Chain[len(bc.state.Chain)-1]
	var txs []Transaction

	coinbase := Transaction{From: "COINBASE", To: miner, Amount: bc.cfg.MiningReward, PubKey: "COINBASE", Sig: "COINBASE"}
	txs = append(txs, coinbase)
	bc.state.Balances[miner] += bc.cfg.MiningReward

	max := bc.cfg.MaxTxPerBlock
	if len(bc.mempool) < max {
		max = len(bc.mempool)
	}
	pending := bc.mempool[:max]
	for _, tx := range pending {
		if err := bc.verifyTx(&tx); err == nil {
			bc.state.Balances[tx.From] -= tx.Amount + tx.Fee
			bc.state.Balances[tx.To] += tx.Amount
			bc.state.Nonces[tx.From]++
			if tx.Fee > 0 {
				bc.state.Balances[miner] += tx.Fee
			}
			txs = append(txs, tx)
		}
	}

	block := &Block{
		Index:        last.Index + 1,
		Timestamp:    time.Now().Unix(),
		Transactions: txs,
		PrevHash:     last.Hash,
		Miner:        miner,
	}
	target := strings.Repeat("0", bc.cfg.Difficulty)
	for block.Nonce = 0; ; block.Nonce++ {
		block.Hash = bc.calcHash(*block)
		if strings.HasPrefix(block.Hash, target) {
			break
		}
	}

	bc.state.Chain = append(bc.state.Chain, *block)
	bc.mempool = bc.mempool[max:]
	bc.save()
	log.Printf("Блок #%d смайнен: %s... (tx=%d, nonce=%d)", block.Index, block.Hash[:16], len(txs), block.Nonce)
	return block, nil
}

func (bc *Blockchain) validateChain() bool {
	for i := 1; i < len(bc.state.Chain); i++ {
		c, p := bc.state.Chain[i], bc.state.Chain[i-1]
		if c.PrevHash != p.Hash || bc.calcHash(c) != c.Hash {
			return false
		}
	}
	return true
}

func (bc *Blockchain) save() {
	data, _ := json.MarshalIndent(bc.state, "", "  ")
	tmp := bc.chainFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		log.Println("Ошибка записи:", err)
		return
	}
	os.Rename(tmp, bc.chainFile)
}

func (bc *Blockchain) load() bool {
	data, err := os.ReadFile(bc.chainFile)
	if err != nil {
		return false
	}
	if err := json.Unmarshal(data, &bc.state); err != nil {
		return false
	}
	if bc.state.Balances == nil {
		bc.state.Balances = make(map[string]int64)
	}
	if bc.state.Nonces == nil {
		bc.state.Nonces = make(map[string]int64)
	}
	log.Printf("Загружено: %d блоков, %d адресов", len(bc.state.Chain), len(bc.state.Balances))
	return true
}

// ===== КОШЛЁК =====

func NewWallet() (*Wallet, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256(pub)
	addr := hex.EncodeToString(h[:])[:40]
	return &Wallet{
		PrivateKey: priv,
		PublicKey:  pub,
		Address:    addr,
		PrivHex:    hex.EncodeToString(priv),
		PubHex:     hex.EncodeToString(pub),
	}, nil
}

func LoadWallet(path string) (*Wallet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var w Wallet
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, err
	}
	pk, _ := hex.DecodeString(w.PrivHex)
	pub, _ := hex.DecodeString(w.PubHex)
	w.PrivateKey = pk
	w.PublicKey = pub
	return &w, nil
}

func (w *Wallet) Save(path string) error {
	data, _ := json.MarshalIndent(w, "", "  ")
	return os.WriteFile(path, data, 0600)
}

func (w *Wallet) SignTx(tx *Transaction) {
	tx.From = w.Address
	tx.PubKey = w.PubHex
	msg := fmt.Sprintf("%s%s%d%d%d%s", tx.From, tx.To, tx.Amount, tx.Fee, tx.Nonce, tx.OrderID)
	sig := ed25519.Sign(w.PrivateKey, []byte(msg))
	tx.Sig = hex.EncodeToString(sig)
}

// ===== HTTP API =====

var (
	bc     *Blockchain
	cfg    *Config
	wallet *Wallet
)

func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cfg.APIKey != "" {
			key := r.Header.Get("X-API-Key")
			if key != cfg.APIKey {
				http.Error(w, `{"error":"unauthorized"}`, 401)
				return
			}
		}
		next(w, r)
	}
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

func handleHealth(w http.ResponseWriter, r *http.Request) {
	jsonResp(w, 200, map[string]string{"status": "ok"})
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	jsonResp(w, 200, map[string]any{
		"blocks":     len(bc.state.Chain),
		"mempool":    len(bc.mempool),
		"balances":   len(bc.state.Balances),
		"difficulty": bc.cfg.Difficulty,
		"reward":     bc.cfg.MiningReward,
	})
}

func handleBalance(w http.ResponseWriter, r *http.Request) {
	addr := r.URL.Query().Get("addr")
	if addr == "" {
		handleError(w, fmt.Errorf("нужен параметр addr"))
		return
	}
	bc.mu.Lock()
	bal := bc.state.Balances[addr]
	nonce := bc.state.Nonces[addr]
	bc.mu.Unlock()
	jsonResp(w, 200, map[string]any{"address": addr, "balance": bal, "nonce": nonce})
}

func handleWalletNew(w http.ResponseWriter, r *http.Request) {
	wlt, err := NewWallet()
	if err != nil {
		handleError(w, err)
		return
	}
	if err := wlt.Save(cfg.DataDir + "/wallet.json"); err != nil {
		handleError(w, err)
		return
	}
	wallet = wlt
	jsonResp(w, 200, map[string]string{
		"address": wlt.Address,
		"message": "кошелёк создан и сохранён в wallet.json",
	})
}

func handleWalletShow(w http.ResponseWriter, r *http.Request) {
	if wallet == nil {
		wl, err := LoadWallet(cfg.DataDir + "/wallet.json")
		if err != nil {
			handleError(w, fmt.Errorf("кошелёк не найден — создайте через /wallet/new"))
			return
		}
		wallet = wl
	}
	jsonResp(w, 200, map[string]string{"address": wallet.Address})
}

func handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		jsonResp(w, 405, map[string]string{"error": "только POST"})
		return
	}
	var tx Transaction
	if err := json.NewDecoder(r.Body).Decode(&tx); err != nil {
		handleError(w, fmt.Errorf("неверный JSON"))
		return
	}
	defer r.Body.Close()
	if tx.Fee == 0 {
		tx.Fee = 1
	}
	if err := bc.addTx(tx); err != nil {
		handleError(w, err)
		return
	}
	jsonResp(w, 200, map[string]string{
		"status":  "принята в мемпул",
		"order":   tx.OrderID,
		"from":    tx.From,
		"to":      tx.To,
		"amount":  fmt.Sprintf("%d", tx.Amount),
	})
}

func handleSendSigned(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		jsonResp(w, 405, map[string]string{"error": "только POST"})
		return
	}
	if wallet == nil {
		wl, err := LoadWallet(cfg.DataDir + "/wallet.json")
		if err != nil {
			handleError(w, fmt.Errorf("кошелёк не найден — создайте через /wallet/new"))
			return
		}
		wallet = wl
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
	bc.mu.Lock()
	nonce := bc.state.Nonces[wallet.Address]
	bc.mu.Unlock()

	tx := Transaction{
		To:      req.To,
		Amount:  req.Amount,
		Fee:     req.Fee,
		Nonce:   nonce,
		OrderID: req.OrderID,
	}
	wallet.SignTx(&tx)
	if err := bc.addTx(tx); err != nil {
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

func handleMine(w http.ResponseWriter, r *http.Request) {
	miner := r.URL.Query().Get("miner")
	if miner == "" {
		if wallet != nil {
			miner = wallet.Address
		} else {
			handleError(w, fmt.Errorf("укажите ?miner=ADDR или создайте кошелёк"))
			return
		}
	}
	block, err := bc.mineBlock(miner)
	if err != nil {
		handleError(w, err)
		return
	}
	jsonResp(w, 200, block)
}

func handleChain(w http.ResponseWriter, r *http.Request) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	jsonResp(w, 200, bc.state.Chain)
}

func handleValidate(w http.ResponseWriter, r *http.Request) {
	ok := bc.validateChain()
	jsonResp(w, 200, map[string]any{"valid": ok, "blocks": len(bc.state.Chain)})
}

func handleMempool(w http.ResponseWriter, r *http.Request) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	jsonResp(w, 200, bc.mempool)
}

func handleTxStatus(w http.ResponseWriter, r *http.Request) {
	orderID := r.URL.Query().Get("order_id")
	if orderID == "" {
		handleError(w, fmt.Errorf("нужен параметр order_id"))
		return
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	for i := len(bc.state.Chain) - 1; i >= 0; i-- {
		for _, tx := range bc.state.Chain[i].Transactions {
			if tx.OrderID == orderID {
				jsonResp(w, 200, map[string]any{
					"confirmed": true,
					"block":     bc.state.Chain[i].Index,
					"tx":        tx,
				})
				return
			}
		}
	}
	for _, tx := range bc.mempool {
		if tx.OrderID == orderID {
			jsonResp(w, 200, map[string]any{"confirmed": false, "status": "в мемпуле"})
			return
		}
	}
	jsonResp(w, 404, map[string]string{"status": "не найдена"})
}

func handleDistribute(w http.ResponseWriter, r *http.Request) {
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
	bc.mu.Lock()
	defer bc.mu.Unlock()
	genesisAddr := "detalki_genesis_owner_0000000000000000000000000000000000"
	if bc.state.Balances[genesisAddr] < req.Amount {
		handleError(w, fmt.Errorf("недостаточно средств на genesis"))
		return
	}
	tx := Transaction{
		From:    genesisAddr,
		To:      req.To,
		Amount:  req.Amount,
		Nonce:   bc.state.Nonces[genesisAddr],
		PubKey:  "GENESIS",
		Sig:     "GENESIS",
	}
	bc.mempool = append(bc.mempool, tx)
	log.Printf("Genesis-распределение: %d DTK -> %s", req.Amount, req.To[:12])
	jsonResp(w, 200, map[string]string{"status": "в мемпуле", "to": req.To, "amount": fmt.Sprintf("%d", req.Amount)})
}

// ===== MAIN =====

func main() {
	cfg = loadConfig()
	bc = NewBlockchain(cfg)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/status", handleStatus)
	mux.HandleFunc("/balance", handleBalance)
	mux.HandleFunc("/wallet/new", handleWalletNew)
	mux.HandleFunc("/wallet", handleWalletShow)
	mux.HandleFunc("/send", authMiddleware(handleSend))
	mux.HandleFunc("/send-signed", authMiddleware(handleSendSigned))
	mux.HandleFunc("/distribute", authMiddleware(handleDistribute))
	mux.HandleFunc("/mine", handleMine)
	mux.HandleFunc("/chain", handleChain)
	mux.HandleFunc("/validate", handleValidate)
	mux.HandleFunc("/mempool", handleMempool)
	mux.HandleFunc("/tx/status", handleTxStatus)

	fmt.Println("============================================")
	fmt.Println("  DetalkiPro Chain — Production Node")
	fmt.Println("============================================")
	fmt.Printf("  Порт:        %s\n", cfg.Port)
	fmt.Printf("  Блоков:      %d\n", len(bc.state.Chain))
	fmt.Printf("  Адресов:     %d\n", len(bc.state.Balances))
	fmt.Printf("  Сложность:   %d нулей\n", cfg.Difficulty)
	fmt.Printf("  Награда:     %d DTK\n", cfg.MiningReward)
	fmt.Printf("  Эмиссия:     %d DTK\n", cfg.GenesisSupply)
	fmt.Printf("  Данные:      %s/chain.json\n", cfg.DataDir)
	if cfg.APIKey != "" {
		fmt.Printf("  API-Key:     включён\n")
	}
	fmt.Println("--------------------------------------------")
	fmt.Println("  GET  /health            — проверка ноды")
	fmt.Println("  GET  /status            — статус сети")
	fmt.Println("  GET  /balance?addr=...  — баланс")
	fmt.Println("  POST /wallet/new        — создать кошелёк")
	fmt.Println("  GET  /wallet            — адрес кошелька")
	fmt.Println("  POST /send              — подписанная TX (вручную)")
	fmt.Println("  POST /send-signed       — подписать и отправить")
	fmt.Println("  POST /distribute        — раздать с genesis")
	fmt.Println("  GET  /mine?miner=...     — майнить блок")
	fmt.Println("  GET  /chain             — вся цепочка")
	fmt.Println("  GET  /validate           — проверить цепь")
	fmt.Println("  GET  /mempool           — мемпул")
	fmt.Println("  GET  /tx/status?order_id=... — статус TX по заказу")
	fmt.Println("============================================")

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: mux}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Останавливаю ноду...")
	bc.save()
	srv.Close()
	log.Println("Нода остановлена. Данные сохранены.")
}
