package config

import (
	"os"
	"strconv"
)

type QdrantConfig struct {
	Host   string
	Port   int
	APIKey string
}

func LoadQdrantConfig() *QdrantConfig {
	host := os.Getenv("QDRANT_HOST")
	if host == "" {
		host = "localhost"
	}
	port := 6334
	if p := os.Getenv("QDRANT_PORT"); p != "" {
		if v, err := strconv.Atoi(p); err == nil {
			port = v
		}
	}
	return &QdrantConfig{
		Host:   host,
		Port:   port,
		APIKey: os.Getenv("QDRANT_API_KEY"),
	}
}
