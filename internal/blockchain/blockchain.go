package blockchain

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
	"sync"
	"time"

	"detalki-chain/internal/models"
	"detalki-chain/internal/storage"
	"detalki-chain/internal/txpool"
)

const fixedExtranonce1 = "00000000"

type Config struct {
	Difficulty    int
	MiningReward  int64
	GenesisSupply int64
	MaxTxPerBlock int
	DataDir       string
}

type Blockchain struct {
	mu          sync.Mutex
	Cfg         *Config
	storage     storage.Storage
	pool        *txpool.TxPool
	state       *models.State
	genesisAddr string
}

type BlockTemplate struct {
	Header []byte
	Block  models.Block
}

func NewBlockchain(cfg *Config, st storage.Storage, pool *txpool.TxPool) *Blockchain {
	bc := &Blockchain{
		Cfg:     cfg,
		storage: st,
		pool:    pool,
	}
	state, err := st.LoadState()
	if err != nil || state == nil {
		state = &models.State{
			Chain:    []models.Block{},
			Balances: make(map[string]int64),
			Nonces:   make(map[string]int64),
		}
	}
	bc.state = state
	if len(bc.state.Chain) == 0 {
		bc.createGenesis()
	}
	return bc
}

func (bc *Blockchain) createGenesis() {
	if bc.genesisAddr == "" {
		bc.genesisAddr = "dtk_genesis_0000000000000000"
	}
	genesisTx := models.Transaction{
		From:   "GENESIS",
		To:     bc.genesisAddr,
		Amount: bc.Cfg.GenesisSupply,
		PubKey: "GENESIS",
		Sig:    "GENESIS",
	}
	g := models.Block{
		Index:        0,
		Timestamp:    time.Now().Unix(),
		Transactions: []models.Transaction{genesisTx},
		PrevHash:     "0000000000000000000000000000000000000000000000000000000000000000",
		Miner:        "genesis",
	}
	g.Hash = bc.calculateHash(g)
	bc.state.Chain = append(bc.state.Chain, g)
	bc.state.Balances[bc.genesisAddr] = bc.Cfg.GenesisSupply
	bc.save()
}

func (bc *Blockchain) calculateHash(b models.Block) string {
	data := fmt.Sprintf("%d%d%v%s%d%s", b.Index, b.Timestamp, b.Transactions, b.PrevHash, b.Nonce, b.Miner)
	h := sha256.Sum256([]byte(data))
	h2 := sha256.Sum256(h[:])
	return hex.EncodeToString(h2[:])
}

func (bc *Blockchain) save() {
	if err := bc.storage.SaveState(bc.state); err != nil {
		log.Printf("Ошибка сохранения состояния: %v", err)
	}
}

// ===== Public API methods =====

func (bc *Blockchain) Status() map[string]any {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	return map[string]any{
		"height":     len(bc.state.Chain),
		"difficulty": bc.Cfg.Difficulty,
		"mempool":    len(bc.pool.All()),
	}
}

func (bc *Blockchain) Balance(addr string) int64 {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	return bc.state.Balances[addr]
}

func (bc *Blockchain) Nonce(addr string) int64 {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	return bc.state.Nonces[addr]
}

func (bc *Blockchain) AddTx(tx models.Transaction) error {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if tx.From == "" || tx.To == "" {
		return fmt.Errorf("from и to обязательны")
	}
	if tx.Amount <= 0 {
		return fmt.Errorf("сумма должна быть положительной")
	}
	if tx.From != "GENESIS" && tx.From != "COINBASE" {
		if bc.state.Balances[tx.From] < tx.Amount+tx.Fee {
			return fmt.Errorf("недостаточно средств")
		}
		if tx.Nonce != bc.state.Nonces[tx.From] {
			return fmt.Errorf("неверный nonce: ожидается %d, получено %d", bc.state.Nonces[tx.From], tx.Nonce)
		}
	}
	bc.pool.Add(tx)
	return nil
}

func (bc *Blockchain) MineBlock(miner string) (*models.Block, error) {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	prevBlock := bc.state.Chain[len(bc.state.Chain)-1]
	txs := bc.pool.All()
	if len(txs) > bc.Cfg.MaxTxPerBlock {
		txs = txs[:bc.Cfg.MaxTxPerBlock]
	}
	coinbaseTx := models.Transaction{
		From:   "COINBASE",
		To:     miner,
		Amount: bc.Cfg.MiningReward,
	}
	allTxs := append([]models.Transaction{coinbaseTx}, txs...)

	block := models.Block{
		Index:        prevBlock.Index + 1,
		Timestamp:    time.Now().Unix(),
		Transactions: allTxs,
		PrevHash:     prevBlock.Hash,
		Miner:        miner,
	}

	difficulty := bc.GetPoCDifficulty(allTxs)
	target := new(big.Int)
	target.Lsh(big.NewInt(1), uint(256-difficulty*4))

	for nonce := int64(0); ; nonce++ {
		block.Nonce = nonce
		hash := bc.calculateHash(block)
		hashInt := new(big.Int)
		hashInt.SetString(hash, 16)
		if hashInt.Cmp(target) <= 0 {
			block.Hash = hash
			break
		}
	}

	bc.state.Balances[miner] += bc.Cfg.MiningReward
	confirmed := make(map[string]bool)
	for i := 1; i < len(block.Transactions); i++ {
		tx := &block.Transactions[i]
		if bc.verifyTx(tx) == nil {
			bc.state.Balances[tx.From] -= tx.Amount + tx.Fee
			bc.state.Balances[tx.To] += tx.Amount
			bc.state.Nonces[tx.From]++
			if tx.Fee > 0 {
				bc.state.Balances[miner] += tx.Fee
			}
			confirmed[tx.OrderID] = true
		}
	}

	remaining := bc.pool.All()
	bc.pool = txpool.NewTxPool()
	for _, tx := range remaining {
		if !confirmed[tx.OrderID] {
			bc.pool.Add(tx)
		}
	}

	bc.state.Chain = append(bc.state.Chain, block)
	bc.save()

	log.Printf("Блок #%d смайнен: %s... (nonce=%d, diff=%d)",
		block.Index, block.Hash[:16], block.Nonce, difficulty)
	return &block, nil
}

func (bc *Blockchain) Chain() []models.Block {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	return bc.state.Chain
}

func (bc *Blockchain) ValidateChain() bool {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	for i := 1; i < len(bc.state.Chain); i++ {
		if bc.state.Chain[i].PrevHash != bc.state.Chain[i-1].Hash {
			return false
		}
	}
	return true
}

func (bc *Blockchain) Mempool() []models.Transaction {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	return bc.pool.All()
}

func (bc *Blockchain) TxStatus(orderID string) (bool, int, *models.Transaction) {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	for _, tx := range bc.pool.All() {
		if tx.OrderID == orderID {
			return false, 0, &tx
		}
	}
	for i, block := range bc.state.Chain {
		for j := range block.Transactions {
			if block.Transactions[j].OrderID == orderID {
				return true, i, &block.Transactions[j]
			}
		}
	}
	return false, 0, nil
}

func (bc *Blockchain) AddGenesisDistribution(to string, amount int64) error {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	if bc.state.Balances[bc.genesisAddr] < amount {
		return fmt.Errorf("недостаточно средств в генезисе")
	}
	tx := models.Transaction{
		From:    bc.genesisAddr,
		To:      to,
		Amount:  amount,
		Fee:     1,
		Nonce:   bc.state.Nonces[bc.genesisAddr],
	}
	bc.pool.Add(tx)
	return nil
}

// ===== PoC methods =====

func (bc *Blockchain) GetPoCDifficulty(txs []models.Transaction) int {
	return bc.EffectiveDifficulty(bc.CountCommerceTx(txs))
}

func (bc *Blockchain) CountCommerceTx(txs []models.Transaction) int {
	count := 0
	for _, tx := range txs {
		if tx.OrderID != "" && tx.From != "COINBASE" && tx.From != "GENESIS" {
			count++
		}
	}
	return count
}

func (bc *Blockchain) EffectiveDifficulty(commerceCount int) int {
	discount := commerceCount / 2
	if discount > 2 {
		discount = 2
	}
	effective := bc.Cfg.Difficulty - discount
	if effective < 1 {
		effective = 1
	}
	return effective
}

func HashMeetsTarget(hashHex string, difficulty int) bool {
	target := new(big.Int)
	target.Lsh(big.NewInt(1), uint(256-difficulty*4))
	hashInt := new(big.Int)
	_, ok := hashInt.SetString(hashHex, 16)
	if !ok {
		return false
	}
	return hashInt.Cmp(target) <= 0
}

func reverseBytes(b []byte) []byte {
	r := make([]byte, len(b))
	for i, v := range b {
		r[len(b)-1-i] = v
	}
	return r
}

func doubleSHA256(data []byte) []byte {
	h1 := sha256.Sum256(data)
	h2 := sha256.Sum256(h1[:])
	return h2[:]
}

func (bc *Blockchain) GetBlockTemplate(miner string) *BlockTemplate {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	prevBlock := bc.state.Chain[len(bc.state.Chain)-1]
	txs := bc.pool.All()
	if len(txs) > bc.Cfg.MaxTxPerBlock {
		txs = txs[:bc.Cfg.MaxTxPerBlock]
	}
	coinbaseTx := models.Transaction{
		From:   "COINBASE",
		To:     miner,
		Amount: bc.Cfg.MiningReward,
	}
	allTxs := append([]models.Transaction{coinbaseTx}, txs...)

	block := models.Block{
		Index:        prevBlock.Index + 1,
		Timestamp:    time.Now().Unix(),
		Transactions: allTxs,
		PrevHash:     prevBlock.Hash,
		Miner:        miner,
	}

	header := make([]byte, 80)
	prevBytes, _ := hex.DecodeString(prevBlock.Hash)
	if len(prevBytes) >= 32 {
		copy(header[0:32], prevBytes)
	}

	return &BlockTemplate{
		Header: header,
		Block:  block,
	}
}

func (bc *Blockchain) SubmitNoncePoC(template *BlockTemplate, nonce, ntime uint32) (*models.Block, bool) {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	header := make([]byte, 80)
	copy(header, template.Header)
	binary.LittleEndian.PutUint32(header[68:72], ntime)
	binary.LittleEndian.PutUint32(header[76:80], nonce)

	hash := doubleSHA256(header)
	hashHex := hex.EncodeToString(reverseBytes(hash))

	difficulty := bc.GetPoCDifficulty(template.Block.Transactions)

	if !HashMeetsTarget(hashHex, difficulty) {
		return nil, false
	}

	block := template.Block
	block.Timestamp = int64(ntime)
	block.Nonce = int64(nonce)
	block.Hash = hashHex

	bc.state.Balances[block.Miner] += bc.Cfg.MiningReward

	confirmed := make(map[string]bool)
	for i := 1; i < len(block.Transactions); i++ {
		tx := &block.Transactions[i]
		if bc.verifyTx(tx) == nil {
			bc.state.Balances[tx.From] -= tx.Amount + tx.Fee
			bc.state.Balances[tx.To] += tx.Amount
			bc.state.Nonces[tx.From]++
			if tx.Fee > 0 {
				bc.state.Balances[block.Miner] += tx.Fee
			}
			confirmed[tx.OrderID] = true
		}
	}

	remaining := bc.pool.All()
	bc.pool = txpool.NewTxPool()
	for _, tx := range remaining {
		if !confirmed[tx.OrderID] {
			bc.pool.Add(tx)
		}
	}

	bc.state.Chain = append(bc.state.Chain, block)
	bc.save()

	log.Printf("Блок #%d смайнен (PoC): %s... (nonce=%d, diff=%d)",
		block.Index, hashHex[:16], nonce, difficulty)
	return &block, true
}

func (bc *Blockchain) verifyTx(tx *models.Transaction) error {
	if tx.From == "GENESIS" || tx.From == "COINBASE" {
		return nil
	}
	if bc.state.Balances[tx.From] < tx.Amount+tx.Fee {
		return fmt.Errorf("недостаточно средств")
	}
	return nil
}
