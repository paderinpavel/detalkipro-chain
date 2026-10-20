package models

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
	PrivateKey []byte `json:"-"`
	PublicKey  []byte `json:"-"`
	Address    string `json:"address"`
	PrivHex    string `json:"private_key"`
	PubHex     string `json:"public_key"`
}
