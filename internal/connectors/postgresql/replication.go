package postgresql

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ReplicationSlotManager manages PostgreSQL replication slots
type ReplicationSlotManager struct {
	pool *pgxpool.Pool
}

// NewReplicationSlotManager creates a new replication slot manager
func NewReplicationSlotManager(pool *pgxpool.Pool) *ReplicationSlotManager {
	return &ReplicationSlotManager{
		pool: pool,
	}
}

// CreateReplicationSlot creates a logical replication slot
func (rsm *ReplicationSlotManager) CreateReplicationSlot(ctx context.Context, slotName, outputPlugin string) (*ReplicationSlot, error) {
	// Check if slot already exists
	slot, err := rsm.GetReplicationSlot(ctx, slotName)
	if err == nil {
		log.Printf("Replication slot %s already exists", slotName)
		return slot, nil
	}

	// Create new replication slot
	var lsn string
	err = rsm.pool.QueryRow(ctx,
		"SELECT lsn FROM pg_create_logical_replication_slot($1, $2, false)",
		slotName, outputPlugin).Scan(&lsn)
	if err != nil {
		return nil, fmt.Errorf("failed to create replication slot %s: %w", slotName, err)
	}

	log.Printf("Created replication slot %s at LSN %s", slotName, lsn)

	// Return the newly created slot information
	return rsm.GetReplicationSlot(ctx, slotName)
}

// GetReplicationSlot retrieves information about a specific replication slot
func (rsm *ReplicationSlotManager) GetReplicationSlot(ctx context.Context, slotName string) (*ReplicationSlot, error) {
	slot := &ReplicationSlot{}
	
	query := `
		SELECT 
			slot_name,
			plugin,
			slot_type,
			datname,
			temporary,
			active,
			COALESCE(restart_lsn::text, '') as restart_lsn,
			COALESCE(confirmed_flush_lsn::text, '') as confirmed_flush_lsn
		FROM pg_replication_slots rs
		LEFT JOIN pg_database d ON rs.database = d.oid
		WHERE slot_name = $1`

	err := rsm.pool.QueryRow(ctx, query, slotName).Scan(
		&slot.SlotName,
		&slot.Plugin,
		&slot.SlotType,
		&slot.Database,
		&slot.Temporary,
		&slot.Active,
		&slot.RestartLSN,
		&slot.ConfirmedFlush,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("replication slot %s not found", slotName)
		}
		return nil, fmt.Errorf("failed to get replication slot %s: %w", slotName, err)
	}

	return slot, nil
}

// ListReplicationSlots lists all replication slots
func (rsm *ReplicationSlotManager) ListReplicationSlots(ctx context.Context) ([]*ReplicationSlot, error) {
	query := `
		SELECT 
			slot_name,
			plugin,
			slot_type,
			datname,
			temporary,
			active,
			COALESCE(restart_lsn::text, '') as restart_lsn,
			COALESCE(confirmed_flush_lsn::text, '') as confirmed_flush_lsn
		FROM pg_replication_slots rs
		LEFT JOIN pg_database d ON rs.database = d.oid
		ORDER BY slot_name`

	rows, err := rsm.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list replication slots: %w", err)
	}
	defer rows.Close()

	var slots []*ReplicationSlot
	for rows.Next() {
		slot := &ReplicationSlot{}
		err := rows.Scan(
			&slot.SlotName,
			&slot.Plugin,
			&slot.SlotType,
			&slot.Database,
			&slot.Temporary,
			&slot.Active,
			&slot.RestartLSN,
			&slot.ConfirmedFlush,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan replication slot: %w", err)
		}
		slots = append(slots, slot)
	}

	return slots, nil
}

// DeleteReplicationSlot drops a replication slot
func (rsm *ReplicationSlotManager) DeleteReplicationSlot(ctx context.Context, slotName string) error {
	// Check if slot exists and is active
	slot, err := rsm.GetReplicationSlot(ctx, slotName)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			log.Printf("Replication slot %s does not exist", slotName)
			return nil // Already deleted
		}
		return err
	}

	if slot.Active {
		return fmt.Errorf("cannot delete active replication slot %s", slotName)
	}

	// Drop the replication slot
	_, err = rsm.pool.Exec(ctx, "SELECT pg_drop_replication_slot($1)", slotName)
	if err != nil {
		return fmt.Errorf("failed to drop replication slot %s: %w", slotName, err)
	}

	log.Printf("Dropped replication slot %s", slotName)
	return nil
}

// MonitorReplicationSlot monitors replication slot health and lag
func (rsm *ReplicationSlotManager) MonitorReplicationSlot(ctx context.Context, slotName string) (*ReplicationSlotStats, error) {
	stats := &ReplicationSlotStats{}
	
	query := `
		SELECT 
			slot_name,
			active,
			COALESCE(restart_lsn::text, '') as restart_lsn,
			COALESCE(confirmed_flush_lsn::text, '') as confirmed_flush_lsn,
			CASE 
				WHEN confirmed_flush_lsn IS NOT NULL AND pg_current_wal_lsn() IS NOT NULL
				THEN pg_wal_lsn_diff(pg_current_wal_lsn(), confirmed_flush_lsn)
				ELSE 0
			END as lag_bytes,
			CASE 
				WHEN active_pid IS NOT NULL 
				THEN (SELECT state FROM pg_stat_activity WHERE pid = active_pid)
				ELSE 'inactive'
			END as connection_state
		FROM pg_replication_slots 
		WHERE slot_name = $1`

	var lagBytes int64
	var connectionState string
	
	err := rsm.pool.QueryRow(ctx, query, slotName).Scan(
		&stats.SlotName,
		&stats.Active,
		&stats.RestartLSN,
		&stats.ConfirmedFlushLSN,
		&lagBytes,
		&connectionState,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("replication slot %s not found", slotName)
		}
		return nil, fmt.Errorf("failed to monitor replication slot %s: %w", slotName, err)
	}

	stats.LagBytes = lagBytes
	stats.ConnectionState = connectionState
	stats.LastChecked = time.Now()

	return stats, nil
}

// AdvanceReplicationSlot advances the confirmed_flush_lsn of a slot
func (rsm *ReplicationSlotManager) AdvanceReplicationSlot(ctx context.Context, slotName, lsn string) error {
	_, err := rsm.pool.Exec(ctx, 
		"SELECT pg_replication_slot_advance($1, $2)", 
		slotName, lsn)
	if err != nil {
		return fmt.Errorf("failed to advance replication slot %s to LSN %s: %w", slotName, lsn, err)
	}

	log.Printf("Advanced replication slot %s to LSN %s", slotName, lsn)
	return nil
}

// ReplicationSlotStats contains statistics about a replication slot
type ReplicationSlotStats struct {
	SlotName           string
	Active             bool
	RestartLSN         string
	ConfirmedFlushLSN  string
	LagBytes           int64
	ConnectionState    string
	LastChecked        time.Time
}

// PublicationManager manages PostgreSQL publications
type PublicationManager struct {
	pool *pgxpool.Pool
}

// NewPublicationManager creates a new publication manager
func NewPublicationManager(pool *pgxpool.Pool) *PublicationManager {
	return &PublicationManager{
		pool: pool,
	}
}

// CreatePublication creates a publication for logical replication
func (pm *PublicationManager) CreatePublication(ctx context.Context, pubName string, tables []string, allTables bool) error {
	// Check if publication already exists
	exists, err := pm.PublicationExists(ctx, pubName)
	if err != nil {
		return err
	}

	if exists {
		log.Printf("Publication %s already exists", pubName)
		return nil
	}

	var createSQL string
	if allTables {
		createSQL = fmt.Sprintf("CREATE PUBLICATION %s FOR ALL TABLES", pubName)
	} else if len(tables) > 0 {
		tableList := strings.Join(tables, ", ")
		createSQL = fmt.Sprintf("CREATE PUBLICATION %s FOR TABLE %s", pubName, tableList)
	} else {
		return fmt.Errorf("must specify either allTables=true or provide table list")
	}

	_, err = pm.pool.Exec(ctx, createSQL)
	if err != nil {
		return fmt.Errorf("failed to create publication %s: %w", pubName, err)
	}

	log.Printf("Created publication %s", pubName)
	return nil
}

// PublicationExists checks if a publication exists
func (pm *PublicationManager) PublicationExists(ctx context.Context, pubName string) (bool, error) {
	var exists bool
	err := pm.pool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_publication WHERE pubname = $1)",
		pubName).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check publication existence: %w", err)
	}
	return exists, nil
}

// AddTableToPublication adds tables to an existing publication
func (pm *PublicationManager) AddTableToPublication(ctx context.Context, pubName string, tables []string) error {
	if len(tables) == 0 {
		return nil
	}

	tableList := strings.Join(tables, ", ")
	alterSQL := fmt.Sprintf("ALTER PUBLICATION %s ADD TABLE %s", pubName, tableList)

	_, err := pm.pool.Exec(ctx, alterSQL)
	if err != nil {
		return fmt.Errorf("failed to add tables to publication %s: %w", pubName, err)
	}

	log.Printf("Added tables %v to publication %s", tables, pubName)
	return nil
}

// RemoveTableFromPublication removes tables from an existing publication
func (pm *PublicationManager) RemoveTableFromPublication(ctx context.Context, pubName string, tables []string) error {
	if len(tables) == 0 {
		return nil
	}

	tableList := strings.Join(tables, ", ")
	alterSQL := fmt.Sprintf("ALTER PUBLICATION %s DROP TABLE %s", pubName, tableList)

	_, err := pm.pool.Exec(ctx, alterSQL)
	if err != nil {
		return fmt.Errorf("failed to remove tables from publication %s: %w", pubName, err)
	}

	log.Printf("Removed tables %v from publication %s", tables, pubName)
	return nil
}

// DeletePublication drops a publication
func (pm *PublicationManager) DeletePublication(ctx context.Context, pubName string) error {
	exists, err := pm.PublicationExists(ctx, pubName)
	if err != nil {
		return err
	}

	if !exists {
		log.Printf("Publication %s does not exist", pubName)
		return nil
	}

	_, err = pm.pool.Exec(ctx, fmt.Sprintf("DROP PUBLICATION %s", pubName))
	if err != nil {
		return fmt.Errorf("failed to drop publication %s: %w", pubName, err)
	}

	log.Printf("Dropped publication %s", pubName)
	return nil
}

// GetPublicationTables returns the tables included in a publication
func (pm *PublicationManager) GetPublicationTables(ctx context.Context, pubName string) ([]string, error) {
	query := `
		SELECT schemaname || '.' || tablename as full_table_name
		FROM pg_publication_tables 
		WHERE pubname = $1
		ORDER BY schemaname, tablename`

	rows, err := pm.pool.Query(ctx, query, pubName)
	if err != nil {
		return nil, fmt.Errorf("failed to get publication tables: %w", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return nil, fmt.Errorf("failed to scan table name: %w", err)
		}
		tables = append(tables, tableName)
	}

	return tables, nil
}
