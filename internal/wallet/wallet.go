package wallet

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"detalki-chain/internal/models"
)

func New() (*models.Wallet, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256(pub)
	addr := hex.EncodeToString(h[:])[:40]
	return &models.Wallet{
		PrivateKey: priv,
		PublicKey:  pub,
		Address:    addr,
		PrivHex:    hex.EncodeToString(priv),
		PubHex:     hex.EncodeToString(pub),
	}, nil
}

func Load(path string) (*models.Wallet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var w models.Wallet
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, err
	}
	pk, _ := hex.DecodeString(w.PrivHex)
	pub, _ := hex.DecodeString(w.PubHex)
	w.PrivateKey = pk
	w.PublicKey = pub
	return &w, nil
}

func Save(w *models.Wallet, path string) error {
	data, _ := json.MarshalIndent(w, "", "  ")
	return os.WriteFile(path, data, 0600)
}

func SignTx(w *models.Wallet, tx *models.Transaction) {
	tx.From = w.Address
	tx.PubKey = w.PubHex
	msg := fmt.Sprintf("%s%s%d%d%d%s", tx.From, tx.To, tx.Amount, tx.Fee, tx.Nonce, tx.OrderID)
	sig := ed25519.Sign(w.PrivateKey, []byte(msg))
	tx.Sig = hex.EncodeToString(sig)
}
