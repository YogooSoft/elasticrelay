#!/bin/bash

cd "$(dirname "$0")"

# Configuration
PID_FILE="./logs/elasticrelay.pid"

echo "Stopping ElasticRelay service..."

# Try to get PID from PID file first
PID=""
if [ -f "$PID_FILE" ]; then
    PID=$(cat "$PID_FILE" 2>/dev/null)
fi

# If no PID file or PID is invalid, try to find the process
if [ -z "$PID" ] || ! kill -0 "$PID" 2>/dev/null; then
    echo "PID file not found or process not running, searching for process..."
    # Try multiple patterns to find the elasticrelay process
    echo "Searching for 'bin/elasticrelay.*-config'..."
    PID=$(pgrep -f "bin/elasticrelay.*-config")
    if [ -z "$PID" ]; then
        echo "Searching for 'elasticrelay.*-config'..."
        PID=$(pgrep -f "elasticrelay.*-config")
    fi
    if [ -z "$PID" ]; then
        echo "Searching for 'elasticrelay'..."
        PID=$(pgrep -f "elasticrelay")
    fi
    
    # If we found multiple processes, show them
    if [ -n "$PID" ]; then
        echo "Found process(es): $PID"
        # If multiple PIDs, take the first one
        PID=$(echo "$PID" | head -n1)
        echo "Using PID: $PID"
    fi
fi

if [ -z "$PID" ]; then
    echo "ElasticRelay service is not running."
    # Clean up stale PID file if it exists
    [ -f "$PID_FILE" ] && rm -f "$PID_FILE"
    exit 0
fi

echo "Found ElasticRelay process with PID: $PID"

# Try to stop gracefully with SIGTERM
echo "Sending SIGTERM signal..."
kill -TERM $PID

# Wait up to 10 seconds for graceful shutdown
for i in {1..10}; do
    if ! kill -0 $PID 2>/dev/null; then
        echo "ElasticRelay service stopped gracefully."
        # Clean up PID file
        [ -f "$PID_FILE" ] && rm -f "$PID_FILE"
        exit 0
    fi
    echo "Waiting for graceful shutdown... ($i/10)"
    sleep 1
done

# If still running, force kill
echo "Graceful shutdown failed, forcing termination..."
kill -KILL $PID 2>/dev/null

# Final check and cleanup
if kill -0 $PID 2>/dev/null; then
    echo "Failed to stop ElasticRelay service (PID: $PID)"
    exit 1
else
    echo "ElasticRelay service stopped successfully."
    # Clean up PID file
    [ -f "$PID_FILE" ] && rm -f "$PID_FILE"
    exit 0
fi
