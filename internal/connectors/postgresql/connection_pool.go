package postgresql

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yogoosoft/elasticrelay/internal/config"
)

// ConnectionPoolConfig contains configuration for PostgreSQL connection pool
type ConnectionPoolConfig struct {
	MaxConns        int32         `json:"max_conns"`
	MinConns        int32         `json:"min_conns"`
	MaxConnLifetime time.Duration `json:"max_conn_lifetime"`
	MaxConnIdleTime time.Duration `json:"max_conn_idle_time"`
	HealthCheckPeriod time.Duration `json:"health_check_period"`
	ConnectTimeout  time.Duration `json:"connect_timeout"`
	CommandTimeout  time.Duration `json:"command_timeout"`
}

// DefaultConnectionPoolConfig returns default connection pool configuration
func DefaultConnectionPoolConfig() *ConnectionPoolConfig {
	return &ConnectionPoolConfig{
		MaxConns:        10,
		MinConns:        2,
		MaxConnLifetime: time.Hour,
		MaxConnIdleTime: 30 * time.Minute,
		HealthCheckPeriod: 5 * time.Minute,
		ConnectTimeout:  10 * time.Second,
		CommandTimeout:  30 * time.Second,
	}
}

// ConnectionPoolManager manages PostgreSQL connection pools
type ConnectionPoolManager struct {
	pools   map[string]*ManagedPool
	mutex   sync.RWMutex
}

// ManagedPool wraps pgxpool.Pool with additional management features
type ManagedPool struct {
	Pool         *pgxpool.Pool
	Config       *ConnectionPoolConfig
	DSN          string
	Name         string
	CreatedAt    time.Time
	LastUsed     time.Time
	Stats        *PoolStats
	healthTicker *time.Ticker
	ctx          context.Context
	cancel       context.CancelFunc
	mutex        sync.RWMutex
}

// PoolStats contains pool statistics
type PoolStats struct {
	TotalConns     int32 `json:"total_conns"`
	IdleConns      int32 `json:"idle_conns"`
	AcquireCount   int64 `json:"acquire_count"`
	AcquireDuration time.Duration `json:"acquire_duration"`
	AcquiredConns  int32 `json:"acquired_conns"`
	CanceledAcquireCount int64 `json:"canceled_acquire_count"`
	ConstructingConns int32 `json:"constructing_conns"`
	EmptyAcquireCount int64 `json:"empty_acquire_count"`
	MaxConns       int32 `json:"max_conns"`
}

// NewConnectionPoolManager creates a new connection pool manager
func NewConnectionPoolManager() *ConnectionPoolManager {
	return &ConnectionPoolManager{
		pools: make(map[string]*ManagedPool),
	}
}

// CreatePool creates a new managed connection pool
func (cpm *ConnectionPoolManager) CreatePool(name string, cfg *config.Config, poolConfig *ConnectionPoolConfig) (*ManagedPool, error) {
	cpm.mutex.Lock()
	defer cpm.mutex.Unlock()

	// Check if pool already exists
	if _, exists := cpm.pools[name]; exists {
		return nil, fmt.Errorf("pool %s already exists", name)
	}

	// Use default config if none provided
	if poolConfig == nil {
		poolConfig = DefaultConnectionPoolConfig()
	}

	// Build DSN
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName)

	// Parse and configure pool
	pgxConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse DSN: %w", err)
	}

	// Apply pool configuration
	pgxConfig.MaxConns = poolConfig.MaxConns
	pgxConfig.MinConns = poolConfig.MinConns
	pgxConfig.MaxConnLifetime = poolConfig.MaxConnLifetime
	pgxConfig.MaxConnIdleTime = poolConfig.MaxConnIdleTime
	pgxConfig.HealthCheckPeriod = poolConfig.HealthCheckPeriod

	// Configure connection settings
	pgxConfig.ConnConfig.ConnectTimeout = poolConfig.ConnectTimeout
	// Note: CommandTimeout is not supported in pgx/v5, use context timeouts instead

	// Add connection config hooks
	pgxConfig.BeforeConnect = func(ctx context.Context, config *pgx.ConnConfig) error {
		log.Printf("Creating new PostgreSQL connection to %s:%d", config.Host, config.Port)
		return nil
	}

	pgxConfig.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		// Set connection parameters
		_, err := conn.Exec(ctx, "SET application_name = 'elasticrelay'")
		if err != nil {
			log.Printf("Warning: failed to set application_name: %v", err)
		}

		// Set timezone
		_, err = conn.Exec(ctx, "SET timezone = 'UTC'")
		if err != nil {
			log.Printf("Warning: failed to set timezone: %v", err)
		}

		// Set statement timeout
		_, err = conn.Exec(ctx, "SET statement_timeout = '30s'")
		if err != nil {
			log.Printf("Warning: failed to set statement_timeout: %v", err)
		}

		log.Printf("PostgreSQL connection configured successfully")
		return nil
	}

	// Create pool
	pool, err := pgxpool.NewWithConfig(context.Background(), pgxConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Test connection
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Create managed pool
	ctx, cancel := context.WithCancel(context.Background())
	managedPool := &ManagedPool{
		Pool:      pool,
		Config:    poolConfig,
		DSN:       dsn,
		Name:      name,
		CreatedAt: time.Now(),
		LastUsed:  time.Now(),
		Stats:     &PoolStats{},
		ctx:       ctx,
		cancel:    cancel,
	}

	// Start health check
	managedPool.startHealthCheck()

	// Store pool
	cpm.pools[name] = managedPool

	log.Printf("Created PostgreSQL connection pool '%s' (max=%d, min=%d)", 
		name, poolConfig.MaxConns, poolConfig.MinConns)

	return managedPool, nil
}

// GetPool retrieves a managed pool by name
func (cpm *ConnectionPoolManager) GetPool(name string) (*ManagedPool, error) {
	cpm.mutex.RLock()
	defer cpm.mutex.RUnlock()

	pool, exists := cpm.pools[name]
	if !exists {
		return nil, fmt.Errorf("pool %s not found", name)
	}

	// Update last used time
	pool.mutex.Lock()
	pool.LastUsed = time.Now()
	pool.mutex.Unlock()

	return pool, nil
}

// RemovePool removes and closes a managed pool
func (cpm *ConnectionPoolManager) RemovePool(name string) error {
	cpm.mutex.Lock()
	defer cpm.mutex.Unlock()

	pool, exists := cpm.pools[name]
	if !exists {
		return fmt.Errorf("pool %s not found", name)
	}

	// Stop health check and close pool
	pool.cancel()
	if pool.healthTicker != nil {
		pool.healthTicker.Stop()
	}
	pool.Pool.Close()

	// Remove from map
	delete(cpm.pools, name)

	log.Printf("Removed PostgreSQL connection pool '%s'", name)
	return nil
}

// ListPools returns all managed pools
func (cpm *ConnectionPoolManager) ListPools() map[string]*ManagedPool {
	cpm.mutex.RLock()
	defer cpm.mutex.RUnlock()

	pools := make(map[string]*ManagedPool)
	for name, pool := range cpm.pools {
		pools[name] = pool
	}

	return pools
}

// CloseAll closes all managed pools
func (cpm *ConnectionPoolManager) CloseAll() {
	cpm.mutex.Lock()
	defer cpm.mutex.Unlock()

	for name, pool := range cpm.pools {
		pool.cancel()
		if pool.healthTicker != nil {
			pool.healthTicker.Stop()
		}
		pool.Pool.Close()
		log.Printf("Closed PostgreSQL connection pool '%s'", name)
	}

	cpm.pools = make(map[string]*ManagedPool)
}

// startHealthCheck starts periodic health checks for the pool
func (mp *ManagedPool) startHealthCheck() {
	if mp.Config.HealthCheckPeriod <= 0 {
		return
	}

	mp.healthTicker = time.NewTicker(mp.Config.HealthCheckPeriod)
	
	go func() {
		defer mp.healthTicker.Stop()
		
		for {
			select {
			case <-mp.ctx.Done():
				return
			case <-mp.healthTicker.C:
				mp.performHealthCheck()
			}
		}
	}()
}

// performHealthCheck performs a health check on the pool
func (mp *ManagedPool) performHealthCheck() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Update statistics
	mp.updateStats()

	// Perform ping
	if err := mp.Pool.Ping(ctx); err != nil {
		log.Printf("Health check failed for pool '%s': %v", mp.Name, err)
		return
	}

	// Test a simple query
	var result int
	if err := mp.Pool.QueryRow(ctx, "SELECT 1").Scan(&result); err != nil {
		log.Printf("Health check query failed for pool '%s': %v", mp.Name, err)
		return
	}

	log.Printf("Health check passed for pool '%s'", mp.Name)
}

// updateStats updates pool statistics
func (mp *ManagedPool) updateStats() {
	mp.mutex.Lock()
	defer mp.mutex.Unlock()

	stat := mp.Pool.Stat()
	mp.Stats = &PoolStats{
		TotalConns:           stat.TotalConns(),
		IdleConns:            stat.IdleConns(),
		AcquireCount:         stat.AcquireCount(),
		AcquireDuration:      stat.AcquireDuration(),
		AcquiredConns:        stat.AcquiredConns(),
		CanceledAcquireCount: stat.CanceledAcquireCount(),
		ConstructingConns:    stat.ConstructingConns(),
		EmptyAcquireCount:    stat.EmptyAcquireCount(),
		MaxConns:             stat.MaxConns(),
	}
}

// GetStats returns current pool statistics
func (mp *ManagedPool) GetStats() *PoolStats {
	mp.mutex.RLock()
	defer mp.mutex.RUnlock()

	// Update stats before returning
	stat := mp.Pool.Stat()
	return &PoolStats{
		TotalConns:           stat.TotalConns(),
		IdleConns:            stat.IdleConns(),
		AcquireCount:         stat.AcquireCount(),
		AcquireDuration:      stat.AcquireDuration(),
		AcquiredConns:        stat.AcquiredConns(),
		CanceledAcquireCount: stat.CanceledAcquireCount(),
		ConstructingConns:    stat.ConstructingConns(),
		EmptyAcquireCount:    stat.EmptyAcquireCount(),
		MaxConns:             stat.MaxConns(),
	}
}

// Acquire acquires a connection from the pool
func (mp *ManagedPool) Acquire(ctx context.Context) (*pgxpool.Conn, error) {
	mp.mutex.Lock()
	mp.LastUsed = time.Now()
	mp.mutex.Unlock()

	return mp.Pool.Acquire(ctx)
}

// Query executes a query using the pool
func (mp *ManagedPool) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	mp.mutex.Lock()
	mp.LastUsed = time.Now()
	mp.mutex.Unlock()

	return mp.Pool.Query(ctx, sql, args...)
}

// QueryRow executes a query that returns a single row
func (mp *ManagedPool) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	mp.mutex.Lock()
	mp.LastUsed = time.Now()
	mp.mutex.Unlock()

	return mp.Pool.QueryRow(ctx, sql, args...)
}

// Exec executes a query without returning rows
func (mp *ManagedPool) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	mp.mutex.Lock()
	mp.LastUsed = time.Now()
	mp.mutex.Unlock()

	return mp.Pool.Exec(ctx, sql, args...)
}

// Begin starts a transaction
func (mp *ManagedPool) Begin(ctx context.Context) (pgx.Tx, error) {
	mp.mutex.Lock()
	mp.LastUsed = time.Now()
	mp.mutex.Unlock()

	return mp.Pool.Begin(ctx)
}

// BeginTx starts a transaction with options
func (mp *ManagedPool) BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error) {
	mp.mutex.Lock()
	mp.LastUsed = time.Now()
	mp.mutex.Unlock()

	return mp.Pool.BeginTx(ctx, txOptions)
}

// Ping pings the database
func (mp *ManagedPool) Ping(ctx context.Context) error {
	mp.mutex.Lock()
	mp.LastUsed = time.Now()
	mp.mutex.Unlock()

	return mp.Pool.Ping(ctx)
}

// Close closes the managed pool
func (mp *ManagedPool) Close() {
	mp.cancel()
	if mp.healthTicker != nil {
		mp.healthTicker.Stop()
	}
	mp.Pool.Close()
}

// ReplicationConnectionManager manages connections specifically for logical replication
type ReplicationConnectionManager struct {
	config    *config.Config
	connMutex sync.Mutex
}

// NewReplicationConnectionManager creates a new replication connection manager
func NewReplicationConnectionManager(cfg *config.Config) *ReplicationConnectionManager {
	return &ReplicationConnectionManager{
		config: cfg,
	}
}

// CreateReplicationConnection creates a connection for logical replication
func (rcm *ReplicationConnectionManager) CreateReplicationConnection(ctx context.Context) (*pgconn.PgConn, error) {
	rcm.connMutex.Lock()
	defer rcm.connMutex.Unlock()

	// Build connection string for replication
	connString := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable replication=database",
		rcm.config.DBHost, rcm.config.DBPort, rcm.config.DBUser, rcm.config.DBPassword, rcm.config.DBName)

	// Create replication connection
	conn, err := pgconn.Connect(ctx, connString)
	if err != nil {
		return nil, fmt.Errorf("failed to create replication connection: %w", err)
	}

	log.Printf("Created PostgreSQL replication connection to %s:%d", rcm.config.DBHost, rcm.config.DBPort)
	return conn, nil
}

// TestReplicationConnection tests if replication is properly configured
func (rcm *ReplicationConnectionManager) TestReplicationConnection(ctx context.Context) error {
	conn, err := rcm.CreateReplicationConnection(ctx)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)

	// Test replication command
	result := conn.Exec(ctx, "IDENTIFY_SYSTEM")
	err = result.Close()
	if err != nil {
		return fmt.Errorf("failed to execute IDENTIFY_SYSTEM: %w", err)
	}

	log.Printf("PostgreSQL replication connection test successful")
	return nil
}
