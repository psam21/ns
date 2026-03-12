package storage

import (
	"context"
	"fmt"

	"github.com/Shugur-Network/relay/internal/logger"
	"go.uber.org/zap"
)

// DatabaseInfo represents database summary information
type DatabaseInfo struct {
	Version   string `json:"version"`
	IsHealthy bool   `json:"is_healthy"`
}

// GetDatabaseInfo retrieves basic database information from PostgreSQL
func (db *DB) GetDatabaseInfo(ctx context.Context) (*DatabaseInfo, error) {
	if !db.isConnected() {
		return nil, fmt.Errorf("database is not connected")
	}

	var version string
	err := db.Pool.QueryRow(ctx, "SELECT version()").Scan(&version)
	if err != nil {
		return nil, fmt.Errorf("failed to query database version: %w", err)
	}

	return &DatabaseInfo{
		Version:   version,
		IsHealthy: true,
	}, nil
}

// GetClusterHealth returns database health information
func (db *DB) GetClusterHealth(ctx context.Context) (map[string]interface{}, error) {
	if !db.isConnected() {
		return nil, fmt.Errorf("database is not connected")
	}

	health := make(map[string]interface{})

	info, err := db.GetDatabaseInfo(ctx)
	if err != nil {
		logger.Warn("Failed to get database info", zap.Error(err))
		health["status"] = "unknown"
		return health, nil
	}

	health["version"] = info.Version
	health["status"] = "healthy"
	health["is_healthy"] = info.IsHealthy

	return health, nil
}
