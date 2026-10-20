package storage

import (
	"encoding/json"
	"os"

	"detalki-chain/internal/models"
)

type Storage interface {
	LoadState() (*models.State, error)
	SaveState(s *models.State) error
}

type JsonStorage struct {
	path string
}

func NewJsonStorage(path string) *JsonStorage {
	return &JsonStorage{path: path}
}

func (s *JsonStorage) LoadState() (*models.State, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}
	var state models.State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	if state.Balances == nil {
		state.Balances = make(map[string]int64)
	}
	if state.Nonces == nil {
		state.Nonces = make(map[string]int64)
	}
	return &state, nil
}

func (s *JsonStorage) SaveState(state *models.State) error {
	tmp := s.path + ".tmp"
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
