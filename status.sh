#!/bin/bash

cd "$(dirname "$0")"

# Configuration
PID_FILE="./logs/elasticrelay.pid"
LOG_FILE="./logs/backend.log"

echo "=== ElasticRelay Service Status ==="
echo ""

# Check PID file
if [ -f "$PID_FILE" ]; then
    PID=$(cat "$PID_FILE" 2>/dev/null)
    echo "PID file: $PID_FILE (PID: $PID)"
    
    # Check if process is actually running
    if kill -0 "$PID" 2>/dev/null; then
        echo "Status: ✅ RUNNING"
        
        # Get process info
        if command -v ps >/dev/null 2>&1; then
            echo "Process info:"
            ps -p "$PID" -o pid,ppid,user,pcpu,pmem,time,command 2>/dev/null || echo "  Could not get process details"
        fi
        
        # Check port
        if command -v netstat >/dev/null 2>&1; then
            echo ""
            echo "Port usage:"
            netstat -an | grep ":50051" | head -5 || echo "  Port 50051 not found in netstat"
        elif command -v lsof >/dev/null 2>&1; then
            echo ""
            echo "Port usage:"
            lsof -i :50051 2>/dev/null || echo "  Port 50051 not found"
        fi
        
    else
        echo "Status: ❌ NOT RUNNING (stale PID file)"
        echo "Action: Run ./start.sh to start the service"
    fi
else
    echo "PID file: Not found"
    
    # Try to find process anyway
    FOUND_PID=$(pgrep -f "elasticrelay.*config.*parallel_config.json")
    if [ -n "$FOUND_PID" ]; then
        echo "Status: ⚠️  RUNNING (no PID file, PID: $FOUND_PID)"
        echo "Note: Service was started manually or PID file was removed"
    else
        echo "Status: ❌ NOT RUNNING"
        echo "Action: Run ./start.sh to start the service"
    fi
fi

# Check log file
echo ""
echo "=== Log Information ==="
if [ -f "$LOG_FILE" ]; then
    echo "Log file: $LOG_FILE"
    echo "Log size: $(du -h "$LOG_FILE" 2>/dev/null | cut -f1 || echo "unknown")"
    echo "Last modified: $(stat -f "%Sm" "$LOG_FILE" 2>/dev/null || stat -c "%y" "$LOG_FILE" 2>/dev/null || echo "unknown")"
    
    echo ""
    echo "Recent log entries (last 5 lines):"
    echo "────────────────────────────────────"
    tail -5 "$LOG_FILE" 2>/dev/null || echo "Could not read log file"
else
    echo "Log file: Not found at $LOG_FILE"
fi

echo ""
echo "=== Quick Commands ==="
echo "Start service:     ./start.sh"
echo "Start in daemon:   ./start.sh -d"
echo "Stop service:      ./stop.sh"
echo "View logs:         tail -f $LOG_FILE"
echo ""
