package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port          string
	DataDir       string
	Difficulty    int
	MiningReward  int64
	GenesisSupply int64
	APIKey        string
	MaxTxPerBlock int
}

func Load() *Config {
	return &Config{
		Port:          getEnv("PORT", "8080"),
		DataDir:       getEnv("DATA_DIR", "."),
		Difficulty:    getIntEnv("DIFFICULTY", 4),
		MiningReward:  getInt64Env("MINING_REWARD", 50),
		GenesisSupply: getInt64Env("GENESIS_SUPPLY", 100000000),
		APIKey:        os.Getenv("API_KEY"),
		MaxTxPerBlock: getIntEnv("MAX_TX_PER_BLOCK", 50),
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getIntEnv(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getInt64Env(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}
