package accountstore

import (
	"database/sql"
	"os"
)

type DatabaseStatistics struct {
	Reader DatabasePoolStatistics `json:"reader"`
	Writer DatabasePoolStatistics `json:"writer"`
	Files  DatabaseFileStatistics `json:"files"`
}

type DatabasePoolStatistics struct {
	Initialized        bool  `json:"initialized"`
	MaxOpenConnections int   `json:"max_open_connections"`
	OpenConnections    int   `json:"open_connections"`
	InUse              int   `json:"in_use"`
	Idle               int   `json:"idle"`
	WaitCount          int64 `json:"wait_count"`
	WaitDurationNanos  int64 `json:"wait_duration_ns"`
}

type DatabaseFileStatistics struct {
	DatabaseBytes     *int64 `json:"database_bytes"`
	WALBytes          *int64 `json:"wal_bytes"`
	SharedMemoryBytes *int64 `json:"shared_memory_bytes"`
}

// Statistics never opens a pool, starts a transaction or runs a SQL/PRAGMA
// query. Pool waits count database/sql connection acquisition, not query time.
func (storage *Database) Statistics() DatabaseStatistics {
	storage.poolMu.Lock()
	writer, reader, readConns := storage.pool, storage.readPool, storage.readConns
	storage.poolMu.Unlock()
	if readConns == 0 {
		readConns = defaultSQLiteReadConns
	}
	poolStats := func(pool *sql.DB, limit int) DatabasePoolStatistics {
		result := DatabasePoolStatistics{MaxOpenConnections: limit}
		if pool == nil {
			return result
		}
		stats := pool.Stats()
		result.Initialized = true
		result.MaxOpenConnections, result.OpenConnections = stats.MaxOpenConnections, stats.OpenConnections
		result.InUse, result.Idle = stats.InUse, stats.Idle
		result.WaitCount, result.WaitDurationNanos = stats.WaitCount, stats.WaitDuration.Nanoseconds()
		return result
	}
	fileBytes := func(path string) *int64 {
		var size int64
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			size = info.Size()
		} else if !os.IsNotExist(err) {
			return nil
		}
		return &size
	}
	return DatabaseStatistics{
		Reader: poolStats(reader, readConns), Writer: poolStats(writer, 1),
		Files: DatabaseFileStatistics{
			DatabaseBytes: fileBytes(storage.databasePath), WALBytes: fileBytes(storage.databasePath + "-wal"),
			SharedMemoryBytes: fileBytes(storage.databasePath + "-shm"),
		},
	}
}
