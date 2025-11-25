package postgresql

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
	pb "github.com/yogoosoft/elasticrelay/api/gateway/v1"
)

// WALMessage represents a parsed WAL message from PostgreSQL
type WALMessage struct {
	LSN         string
	Type        WALMessageType
	Timestamp   time.Time
	Transaction *TransactionInfo
	Relation    *RelationInfo
	Data        *RowData
}

// WALMessageType represents the type of WAL message
type WALMessageType int

const (
	WALMessageTypeUnknown WALMessageType = iota
	WALMessageTypeBegin
	WALMessageTypeCommit
	WALMessageTypeRelation
	WALMessageTypeInsert
	WALMessageTypeUpdate
	WALMessageTypeDelete
	WALMessageTypeTruncate
)

// String returns string representation of WAL message type
func (wmt WALMessageType) String() string {
	switch wmt {
	case WALMessageTypeBegin:
		return "BEGIN"
	case WALMessageTypeCommit:
		return "COMMIT"
	case WALMessageTypeRelation:
		return "RELATION"
	case WALMessageTypeInsert:
		return "INSERT"
	case WALMessageTypeUpdate:
		return "UPDATE"
	case WALMessageTypeDelete:
		return "DELETE"
	case WALMessageTypeTruncate:
		return "TRUNCATE"
	default:
		return "UNKNOWN"
	}
}

// TransactionInfo contains information about a transaction
type TransactionInfo struct {
	XID        uint32
	CommitTime time.Time
	BeginLSN   string
	CommitLSN  string
	FinalLSN   string
}

// RelationInfo contains information about a table/relation
type RelationInfo struct {
	RelationID      uint32
	Namespace       string
	RelationName    string
	ReplicaIdentity byte
	Columns         []ColumnInfo
}

// ColumnInfo contains information about a column
type ColumnInfo struct {
	Flags    uint8
	Name     string
	TypeID   uint32
	TypeMod  int32
	TypeName string
}

// RowData contains the actual data for INSERT/UPDATE/DELETE operations
type RowData struct {
	RelationID uint32
	TupleType  byte // 'N' for new, 'O' for old, 'K' for key
	Columns    []ColumnData
}

// ColumnData contains data for a single column
type ColumnData struct {
	Name     string
	Value    interface{}
	IsNull   bool
	TypeID   uint32
	TypeName string
}

// WALParser handles parsing of PostgreSQL logical replication messages
type WALParser struct {
	conn         *pgconn.PgConn
	slotName     string
	publication  string
	startLSN     string
	relations    map[uint32]*RelationInfo
	currentTxn   *TransactionInfo
	tableFilters []string
	typeMapper   *TypeMapper
}

// NewWALParser creates a new WAL parser
func NewWALParser(conn *pgconn.PgConn, slotName, publication, startLSN string, tableFilters []string) *WALParser {
	return &WALParser{
		conn:         conn,
		slotName:     slotName,
		publication:  publication,
		startLSN:     startLSN,
		relations:    make(map[uint32]*RelationInfo),
		tableFilters: tableFilters,
		typeMapper:   NewTypeMapper(),
	}
}

// AddRelation adds a preloaded relation/table schema to the parser
func (wp *WALParser) AddRelation(relation *RelationInfo) {
	wp.relations[relation.RelationID] = relation
}

// StartReplication begins logical replication and parses messages
func (wp *WALParser) StartReplication(ctx context.Context, handler *EventHandler) error {
	// Build replication command with correct syntax
	// All options should be in a single parentheses, comma-separated
	cmd := fmt.Sprintf("START_REPLICATION SLOT %s LOGICAL %s (\"publication_names\" '%s', \"proto_version\" '1', \"messages\" 'true')",
		wp.slotName, wp.startLSN, wp.publication)

	log.Printf("Starting logical replication with command: %s", cmd)

	// Send replication command using SimpleQuery protocol
	log.Printf("[DEBUG] About to send replication command using SimpleQuery")

	// Create a Query message
	queryMsg := &pgproto3.Query{String: cmd}

	// Encode the message
	buf := make([]byte, 0, 256)
	buf, err := queryMsg.Encode(buf)
	if err != nil {
		return fmt.Errorf("failed to encode replication command: %w", err)
	}

	// Write directly to the connection
	log.Printf("[DEBUG] Writing query message to connection")
	_, err = wp.conn.Conn().Write(buf)
	if err != nil {
		return fmt.Errorf("failed to send replication command: %w", err)
	}

	log.Printf("[DEBUG] Command sent, waiting for CopyBothResponse")

	// Now receive the response - should be CopyBothResponse
	initialMsg, err := wp.conn.ReceiveMessage(ctx)
	if err != nil {
		return fmt.Errorf("failed to receive initial response: %w", err)
	}
	log.Printf("[DEBUG] Received initial message type: %T", initialMsg)

	// Verify it's a CopyBothResponse
	if _, ok := initialMsg.(*pgproto3.CopyBothResponse); !ok {
		return fmt.Errorf("unexpected initial response: %T, expected CopyBothResponse", initialMsg)
	}

	log.Printf("[DEBUG] Successfully entered replication mode (got CopyBothResponse)")

	// Process replication messages
	return wp.processMessages(ctx, handler)
}

// processMessages processes incoming WAL messages
func (wp *WALParser) processMessages(ctx context.Context, handler *EventHandler) error {
	log.Printf("[DEBUG] Entering processMessages function")

	// Create a ticker for periodic keepalive messages (every 10 seconds - more frequent)
	keepaliveTicker := time.NewTicker(10 * time.Second)
	defer keepaliveTicker.Stop()
	log.Printf("[DEBUG] Created keepalive ticker")

	// Track the last received LSN for status updates - initialize with starting LSN
	lastReceivedLSN, err := wp.parseLSNToUint64(wp.startLSN)
	if err != nil {
		log.Printf("Warning: failed to parse starting LSN %s: %v", wp.startLSN, err)
		lastReceivedLSN = 0
	}
	log.Printf("[DEBUG] Parsed starting LSN: %s -> %d", wp.startLSN, lastReceivedLSN)

	// Send initial keepalive to establish the connection properly
	log.Printf("Sending initial keepalive message")
	if err := wp.sendStandbyStatusUpdate(ctx, lastReceivedLSN, lastReceivedLSN, lastReceivedLSN); err != nil {
		log.Printf("Failed to send initial keepalive: %v", err)
		return fmt.Errorf("failed to send initial keepalive: %w", err)
	}
	log.Printf("[DEBUG] Initial keepalive sent successfully")

	// Create a channel to receive messages asynchronously
	msgChan := make(chan pgproto3.BackendMessage, 1)
	errChan := make(chan error, 1)
	log.Printf("[DEBUG] Created message channels")

	// Start a goroutine to continuously read messages
	go func() {
		log.Printf("[DEBUG] Starting message receiving goroutine")
		// Add a small delay before starting to receive messages
		time.Sleep(200 * time.Millisecond)
		log.Printf("[DEBUG] Starting to receive messages after delay")

		for {
			log.Printf("[DEBUG] Waiting to receive message...")

			// Create a context with timeout for each receive operation
			receiveCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			msg, err := wp.conn.ReceiveMessage(receiveCtx)
			cancel()

			if err != nil {
				log.Printf("[DEBUG] ReceiveMessage error: %v", err)
				// If it's a timeout or context error, continue the loop
				if receiveCtx.Err() != nil {
					log.Printf("[DEBUG] Receive timeout, continuing...")
					continue
				}
				select {
				case errChan <- err:
				case <-ctx.Done():
					return
				}
				return
			}
			log.Printf("[DEBUG] Received message type: %T", msg)
			select {
			case msgChan <- msg:
			case <-ctx.Done():
				return
			}
		}
	}()

	log.Printf("[DEBUG] Starting main message processing loop")
	for {
		select {
		case <-ctx.Done():
			log.Printf("[DEBUG] Context cancelled, exiting processMessages")
			return ctx.Err()
		case <-keepaliveTicker.C:
			// Send periodic keepalive to prevent timeout
			log.Printf("Sending periodic keepalive message")
			if err := wp.sendStandbyStatusUpdate(ctx, lastReceivedLSN, lastReceivedLSN, lastReceivedLSN); err != nil {
				log.Printf("Failed to send periodic keepalive: %v", err)
			}
		case msg := <-msgChan:
			log.Printf("[DEBUG] Processing message from channel: %T", msg)
			// Process the message based on its type
			if err := wp.handleMessage(msg, handler); err != nil {
				log.Printf("Error handling message: %v", err)
				continue
			}

			// Update last received LSN from XLogData messages
			if copyData, ok := msg.(*pgproto3.CopyData); ok && len(copyData.Data) > 0 {
				if copyData.Data[0] == 'w' && len(copyData.Data) >= 25 {
					// Extract walStart and walEnd from XLogData message
					// Message format: 'w' + walStart(8) + walEnd(8) + sendTime(8) + data
					walStart := binary.BigEndian.Uint64(copyData.Data[1:9])
					walEnd := binary.BigEndian.Uint64(copyData.Data[9:17])

					// Use walStart as the LSN position (walEnd is usually 0 in streaming mode)
					if walStart > 0 {
						lastReceivedLSN = walStart
					} else if walEnd > 0 {
						lastReceivedLSN = walEnd
					}

					log.Printf("[DEBUG] LSN update: walStart=%X/%X, walEnd=%X/%X, using LSN=%X/%X",
						uint32(walStart>>32), uint32(walStart),
						uint32(walEnd>>32), uint32(walEnd),
						uint32(lastReceivedLSN>>32), uint32(lastReceivedLSN))
				}
			}
		case err := <-errChan:
			log.Printf("[DEBUG] Received error from channel: %v", err)
			return fmt.Errorf("failed to receive message: %w", err)
		}
	}
}

// handleMessage processes a single message from PostgreSQL
func (wp *WALParser) handleMessage(msg pgproto3.BackendMessage, handler *EventHandler) error {
	log.Printf("[DEBUG] handleMessage called with type: %T", msg)
	switch m := msg.(type) {
	case *pgproto3.CopyData:
		log.Printf("[DEBUG] Processing CopyData message, length: %d", len(m.Data))
		return wp.processCopyData(m.Data, handler)
	case *pgproto3.ErrorResponse:
		log.Printf("[DEBUG] Received ErrorResponse: %s", m.Message)
		return fmt.Errorf("PostgreSQL error: %s", m.Message)
	case *pgproto3.NoticeResponse:
		log.Printf("PostgreSQL notice: %s", m.Message)
	default:
		log.Printf("Received unhandled message type: %T", msg)
	}
	return nil
}

// processCopyData processes CopyData messages containing WAL data
func (wp *WALParser) processCopyData(data []byte, handler *EventHandler) error {
	if len(data) == 0 {
		log.Printf("[DEBUG] processCopyData: empty data")
		return nil
	}

	msgType := data[0]
	log.Printf("[DEBUG] processCopyData: message type '%c' (0x%02x), data length: %d", msgType, msgType, len(data))

	switch msgType {
	case 'w': // XLogData message
		log.Printf("[DEBUG] Processing XLogData message")
		return wp.parseXLogData(data[1:], handler)
	case 'k': // Primary keepalive message
		log.Printf("[DEBUG] Processing primary keepalive message")
		return wp.parsePrimaryKeepalive(data[1:])
	default:
		log.Printf("Unknown copy data message type: %c (0x%02x)", msgType, msgType)
	}

	return nil
}

// parseXLogData parses XLogData messages containing actual WAL records
func (wp *WALParser) parseXLogData(data []byte, handler *EventHandler) error {
	if len(data) < 24 {
		return fmt.Errorf("XLogData message too short")
	}

	// Parse WAL data header
	walStart := binary.BigEndian.Uint64(data[0:8])
	walEnd := binary.BigEndian.Uint64(data[8:16])
	sendTime := int64(binary.BigEndian.Uint64(data[16:24]))

	log.Printf("[DEBUG] XLogData: walStart=%X/%X, walEnd=%X/%X, sendTime=%d",
		uint32(walStart>>32), uint32(walStart),
		uint32(walEnd>>32), uint32(walEnd),
		sendTime)

	// Parse logical decoding message
	if len(data) > 24 {
		return wp.parseLogicalMessage(data[24:], handler)
	}

	return nil
}

// parseLogicalMessage parses logical decoding messages
func (wp *WALParser) parseLogicalMessage(data []byte, handler *EventHandler) error {
	if len(data) == 0 {
		log.Printf("[DEBUG] parseLogicalMessage: empty data")
		return nil
	}

	msgType := data[0]
	log.Printf("[DEBUG] parseLogicalMessage: message type '%c' (0x%02x), data length: %d", msgType, msgType, len(data))

	switch msgType {
	case 'B': // Begin transaction
		log.Printf("[DEBUG] Parsing BEGIN transaction message")
		return wp.parseBegin(data[1:])
	case 'C': // Commit transaction
		log.Printf("[DEBUG] Parsing COMMIT transaction message")
		return wp.parseCommit(data[1:])
	case 'R': // Relation
		log.Printf("[DEBUG] Parsing RELATION message")
		return wp.parseRelation(data[1:])
	case 'I': // Insert
		log.Printf("[DEBUG] Parsing INSERT message")
		return wp.parseInsert(data[1:], handler)
	case 'U': // Update
		log.Printf("[DEBUG] Parsing UPDATE message")
		return wp.parseUpdate(data[1:], handler)
	case 'D': // Delete
		log.Printf("[DEBUG] Parsing DELETE message")
		return wp.parseDelete(data[1:], handler)
	case 'T': // Truncate
		log.Printf("[DEBUG] Parsing TRUNCATE message")
		return wp.parseTruncate(data[1:], handler)
	default:
		log.Printf("Unknown logical message type: '%c' (0x%02x), first 32 bytes: %v", msgType, msgType, data[:min(32, len(data))])
	}

	return nil
}

// Helper function for min
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// parseBegin parses BEGIN transaction message
func (wp *WALParser) parseBegin(data []byte) error {
	if len(data) < 20 {
		return fmt.Errorf("BEGIN message too short")
	}

	finalLSN := binary.BigEndian.Uint64(data[0:8])
	commitTime := int64(binary.BigEndian.Uint64(data[8:16]))
	xid := binary.BigEndian.Uint32(data[16:20])

	wp.currentTxn = &TransactionInfo{
		XID:        xid,
		CommitTime: time.Unix(commitTime/1000000, (commitTime%1000000)*1000),
		FinalLSN:   fmt.Sprintf("%X/%X", uint32(finalLSN>>32), uint32(finalLSN)),
	}

	return nil
}

// parseCommit parses COMMIT transaction message
func (wp *WALParser) parseCommit(data []byte) error {
	if len(data) < 20 {
		return fmt.Errorf("COMMIT message too short")
	}

	// Reset current transaction
	wp.currentTxn = nil

	return nil
}

// parseRelation parses RELATION message
func (wp *WALParser) parseRelation(data []byte) error {
	if len(data) < 7 {
		return fmt.Errorf("RELATION message too short")
	}

	relationID := binary.BigEndian.Uint32(data[0:4])
	offset := 4

	// Parse namespace (null-terminated string)
	namespaceEnd := offset
	for namespaceEnd < len(data) && data[namespaceEnd] != 0 {
		namespaceEnd++
	}
	if namespaceEnd >= len(data) {
		return fmt.Errorf("RELATION message: namespace not null-terminated")
	}
	namespace := string(data[offset:namespaceEnd])
	offset = namespaceEnd + 1 // Skip null terminator

	// Parse relation name (null-terminated string)
	relationNameEnd := offset
	for relationNameEnd < len(data) && data[relationNameEnd] != 0 {
		relationNameEnd++
	}
	if relationNameEnd >= len(data) {
		return fmt.Errorf("RELATION message: relation name not null-terminated")
	}
	relationName := string(data[offset:relationNameEnd])
	offset = relationNameEnd + 1 // Skip null terminator

	// Parse replica identity
	if offset >= len(data) {
		return fmt.Errorf("RELATION message too short for replica identity")
	}
	replicaIdentity := data[offset]
	offset++

	// Parse column count
	if offset+2 > len(data) {
		return fmt.Errorf("RELATION message too short for column count")
	}
	columnCount := binary.BigEndian.Uint16(data[offset : offset+2])
	offset += 2

	columns := make([]ColumnInfo, 0, columnCount)

	for i := uint16(0); i < columnCount; i++ {
		if offset+1 > len(data) {
			break
		}

		flags := data[offset]
		offset++

		// Parse column name (null-terminated string)
		columnNameEnd := offset
		for columnNameEnd < len(data) && data[columnNameEnd] != 0 {
			columnNameEnd++
		}
		if columnNameEnd >= len(data) {
			break
		}
		columnName := string(data[offset:columnNameEnd])
		offset = columnNameEnd + 1 // Skip null terminator

		// Parse type ID and type mod
		if offset+8 > len(data) {
			break
		}
		typeID := binary.BigEndian.Uint32(data[offset : offset+4])
		typeMod := int32(binary.BigEndian.Uint32(data[offset+4 : offset+8]))
		offset += 8

		columns = append(columns, ColumnInfo{
			Flags:   flags,
			Name:    columnName,
			TypeID:  typeID,
			TypeMod: typeMod,
		})
	}

	wp.relations[relationID] = &RelationInfo{
		RelationID:      relationID,
		Namespace:       namespace,
		RelationName:    relationName,
		ReplicaIdentity: replicaIdentity,
		Columns:         columns,
	}

	log.Printf("[DEBUG] Parsed RELATION: id=%d, schema=%s, table=%s, columns=%d",
		relationID, namespace, relationName, len(columns))

	return nil
}

// parseInsert parses INSERT message
func (wp *WALParser) parseInsert(data []byte, handler *EventHandler) error {
	if len(data) < 5 {
		return fmt.Errorf("INSERT message too short")
	}

	relationID := binary.BigEndian.Uint32(data[0:4])
	tupleType := data[4]

	if tupleType != 'N' {
		return fmt.Errorf("unexpected tuple type for INSERT: %c", tupleType)
	}

	rowData, err := wp.parseTupleData(data[5:], relationID, tupleType)
	if err != nil {
		return fmt.Errorf("failed to parse INSERT tuple data: %w", err)
	}

	return wp.createChangeEvent("INSERT", "", relationID, rowData, nil, handler)
}

// parseUpdate parses UPDATE message
func (wp *WALParser) parseUpdate(data []byte, handler *EventHandler) error {
	if len(data) < 5 {
		return fmt.Errorf("UPDATE message too short")
	}

	relationID := binary.BigEndian.Uint32(data[0:4])

	var oldData, newData *RowData
	var err error
	offset := 4

	// Parse old tuple (if present)
	if offset < len(data) {
		tupleType := data[offset]
		offset++

		if tupleType == 'O' || tupleType == 'K' {
			oldData, err = wp.parseTupleData(data[offset:], relationID, tupleType)
			if err != nil {
				return fmt.Errorf("failed to parse UPDATE old tuple data: %w", err)
			}

			// Find next tuple marker
			for i := offset; i < len(data)-1; i++ {
				if data[i] == 'N' {
					offset = i + 1
					break
				}
			}
		} else if tupleType == 'N' {
			// No old tuple, this is the new tuple
			newData, err = wp.parseTupleData(data[offset:], relationID, tupleType)
			if err != nil {
				return fmt.Errorf("failed to parse UPDATE new tuple data: %w", err)
			}
		}
	}

	// Parse new tuple
	if newData == nil && offset < len(data) {
		newData, err = wp.parseTupleData(data[offset:], relationID, 'N')
		if err != nil {
			return fmt.Errorf("failed to parse UPDATE new tuple data: %w", err)
		}
	}

	return wp.createChangeEvent("UPDATE", "", relationID, newData, oldData, handler)
}

// parseDelete parses DELETE message
func (wp *WALParser) parseDelete(data []byte, handler *EventHandler) error {
	if len(data) < 5 {
		return fmt.Errorf("DELETE message too short")
	}

	relationID := binary.BigEndian.Uint32(data[0:4])
	tupleType := data[4]

	if tupleType != 'O' && tupleType != 'K' {
		return fmt.Errorf("unexpected tuple type for DELETE: %c", tupleType)
	}

	rowData, err := wp.parseTupleData(data[5:], relationID, tupleType)
	if err != nil {
		return fmt.Errorf("failed to parse DELETE tuple data: %w", err)
	}

	return wp.createChangeEvent("DELETE", "", relationID, nil, rowData, handler)
}

// parseTruncate parses TRUNCATE message
func (wp *WALParser) parseTruncate(data []byte, handler *EventHandler) error {
	if len(data) < 6 {
		return fmt.Errorf("TRUNCATE message too short")
	}

	relationCount := binary.BigEndian.Uint32(data[0:4])

	for i := uint32(0); i < relationCount; i++ {
		offset := 4 + i*4
		if len(data) < int(offset+4) {
			break
		}

		relationID := binary.BigEndian.Uint32(data[offset : offset+4])
		err := wp.createChangeEvent("TRUNCATE", "", relationID, nil, nil, handler)
		if err != nil {
			log.Printf("Failed to create TRUNCATE event for relation %d: %v", relationID, err)
		}
	}

	return nil
}

// parseTupleData parses tuple data from WAL messages
func (wp *WALParser) parseTupleData(data []byte, relationID uint32, tupleType byte) (*RowData, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("tuple data too short")
	}

	columnCount := binary.BigEndian.Uint16(data[0:2])
	columns := make([]ColumnData, 0, columnCount)

	relation := wp.relations[relationID]
	if relation == nil {
		return nil, fmt.Errorf("unknown relation ID: %d", relationID)
	}

	offset := 2
	for i := uint16(0); i < columnCount && i < uint16(len(relation.Columns)); i++ {
		if offset >= len(data) {
			break
		}

		columnInfo := relation.Columns[i]
		columnType := data[offset]
		offset++

		var value interface{}
		var isNull bool

		switch columnType {
		case 'n': // NULL value
			isNull = true
			value = nil
		case 't': // Text value
			if offset+4 > len(data) {
				break
			}
			length := binary.BigEndian.Uint32(data[offset : offset+4])
			offset += 4

			if offset+int(length) > len(data) {
				break
			}

			value = string(data[offset : offset+int(length)])
			offset += int(length)
		case 'u': // Unchanged TOAST value
			// Skip unchanged TOAST values
			continue
		default:
			log.Printf("Unknown column type: %c", columnType)
			continue
		}

		columns = append(columns, ColumnData{
			Name:     columnInfo.Name,
			Value:    value,
			IsNull:   isNull,
			TypeID:   columnInfo.TypeID,
			TypeName: columnInfo.TypeName,
		})
	}

	return &RowData{
		RelationID: relationID,
		TupleType:  tupleType,
		Columns:    columns,
	}, nil
}

// createChangeEvent creates a change event from parsed WAL data
func (wp *WALParser) createChangeEvent(operation, lsn string, relationID uint32, newData, oldData *RowData, handler *EventHandler) error {
	relation := wp.relations[relationID]
	if relation == nil {
		return fmt.Errorf("unknown relation ID: %d", relationID)
	}

	// Check table filters
	tableName := relation.RelationName
	if len(wp.tableFilters) > 0 {
		found := false
		for _, filter := range wp.tableFilters {
			if filter == tableName {
				found = true
				break
			}
		}
		if !found {
			return nil // Skip filtered table
		}
	}

	// Use new data if available, otherwise old data
	data := newData
	if data == nil {
		data = oldData
	}
	if data == nil {
		return fmt.Errorf("no data available for change event")
	}

	// Convert to JSON
	dataMap := make(map[string]interface{})
	dataMap["_table"] = tableName
	dataMap["_schema"] = relation.Namespace

	for _, col := range data.Columns {
		// Use TypeMapper to convert PostgreSQL values to Elasticsearch-compatible format
		convertedValue, err := wp.typeMapper.ConvertValue(col.Value, col.TypeID)
		if err != nil {
			log.Printf("Warning: failed to convert column '%s' (type OID %d): %v, using raw value",
				col.Name, col.TypeID, err)
			dataMap[col.Name] = col.Value
		} else {
			dataMap[col.Name] = convertedValue
		}
	}

	jsonData, err := json.Marshal(dataMap)
	if err != nil {
		return fmt.Errorf("failed to marshal data to JSON: %w", err)
	}

	// Get primary key (simplified - would need proper primary key detection)
	primaryKey := ""
	if len(data.Columns) > 0 {
		primaryKey = fmt.Sprintf("%v", data.Columns[0].Value)
	}

	// Create change event
	changeEvent := &pb.ChangeEvent{
		Op: operation,
		Checkpoint: &pb.Checkpoint{
			PostgresLsn: lsn,
		},
		PrimaryKey: primaryKey,
		Data:       string(jsonData),
	}

	// Send to handler
	return handler.stream.Send(changeEvent)
}

// parsePrimaryKeepalive parses a primary keepalive message
func (wp *WALParser) parsePrimaryKeepalive(data []byte) error {
	if len(data) < 17 {
		return fmt.Errorf("primary keepalive message too short")
	}

	// Parse keepalive data
	walEnd := binary.BigEndian.Uint64(data[0:8])
	sendTime := int64(binary.BigEndian.Uint64(data[8:16]))
	replyRequested := data[16] != 0

	_ = sendTime

	if replyRequested {
		// Send standby status update
		return wp.sendStandbyStatusUpdate(context.Background(), walEnd, walEnd, walEnd)
	}

	return nil
}

// sendStandbyStatusUpdate sends a standby status update message
func (wp *WALParser) sendStandbyStatusUpdate(ctx context.Context, received, flushed, applied uint64) error {
	// Build standby status update message
	msg := make([]byte, 34)
	msg[0] = 'r' // Standby status update

	// Set LSN positions
	binary.BigEndian.PutUint64(msg[1:9], received)  // Last received LSN
	binary.BigEndian.PutUint64(msg[9:17], flushed)  // Last flushed LSN
	binary.BigEndian.PutUint64(msg[17:25], applied) // Last applied LSN

	// Set timestamp (PostgreSQL epoch: 2000-01-01 00:00:00 UTC)
	pgEpoch := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	now := time.Since(pgEpoch).Microseconds()
	binary.BigEndian.PutUint64(msg[25:33], uint64(now))

	msg[33] = 0 // Reply not requested

	// Send the standby status update to PostgreSQL
	log.Printf("Sending standby status update: received=%X/%X, flushed=%X/%X, applied=%X/%X",
		uint32(received>>32), uint32(received),
		uint32(flushed>>32), uint32(flushed),
		uint32(applied>>32), uint32(applied))

	// Send CopyData message with standby status update using correct method
	// Create a CopyData message and send it via the connection
	copyData := &pgproto3.CopyData{Data: msg}

	// Convert to wire format and send
	buf := make([]byte, 0, len(msg)+5)
	var err error
	buf, err = copyData.Encode(buf)
	if err != nil {
		return fmt.Errorf("failed to encode standby status update: %w", err)
	}

	// Check context before writing
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context cancelled before sending standby status: %w", err)
	}

	// Write directly to the connection
	_, err = wp.conn.Conn().Write(buf)
	if err != nil {
		return fmt.Errorf("failed to send standby status update: %w", err)
	}

	return nil
}

// parseLSNToUint64 converts PostgreSQL LSN string format (e.g., "0/19A6E88") to uint64
func (wp *WALParser) parseLSNToUint64(lsnStr string) (uint64, error) {
	if lsnStr == "" {
		return 0, fmt.Errorf("empty LSN string")
	}

	parts := strings.Split(lsnStr, "/")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid LSN format: %s", lsnStr)
	}

	// Parse high 32 bits
	high, err := strconv.ParseUint(parts[0], 16, 32)
	if err != nil {
		return 0, fmt.Errorf("failed to parse LSN high part %s: %w", parts[0], err)
	}

	// Parse low 32 bits
	low, err := strconv.ParseUint(parts[1], 16, 32)
	if err != nil {
		return 0, fmt.Errorf("failed to parse LSN low part %s: %w", parts[1], err)
	}

	// Combine into 64-bit LSN
	return (high << 32) | low, nil
}
