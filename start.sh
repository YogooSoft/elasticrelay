#!/bin/bash

cd "$(dirname "$0")"

# Secrets such as NACOS_PASSWORD go in ./.env (git-ignored), e.g. NACOS_PASSWORD=xxx
if [ -f ./.env ]; then
    set -a
    . ./.env
    set +a
fi

# Configuration source: "file" (local config files below) or "nacos" (Nacos config center, hot reload)
CONFIG_SOURCE="${ELASTICRELAY_CONFIG_SOURCE:-nacos}"

# Nacos settings (used when CONFIG_SOURCE=nacos; environment variables take precedence)
NACOS_SERVER_ADDR="${NACOS_SERVER_ADDR:-http://172.168.0.141/}"
NACOS_NAMESPACE="${NACOS_NAMESPACE:-esrelay}"    # namespace ID, not its display name
NACOS_GROUP="${NACOS_GROUP:-test}"
NACOS_DATA_ID="${NACOS_DATA_ID:-mysql_config.json}"
NACOS_TRANSFORM_DATA_ID="${NACOS_TRANSFORM_DATA_ID:-mysql_transform.json}"   # empty = no transform
NACOS_USERNAME="${NACOS_USERNAME:-nacos}"

# Configuration
BINARY="./bin/elasticrelay-darwin-arm64"
#CONFIG="./config/postgresql_config.json"
CONFIG="./config/mysql_config.json"
# CONFIG="./config/mongodb_config.json"
PORT="50051"
LOG_DIR="./logs"
PID_FILE="./logs/elasticrelay.pid"

# Transform configuration (optional)
# Set to empty string to disable transform rules (pass-through mode)
TRANSFORM_CONFIG="./config/mysql_transform.json"
#TRANSFORM_CONFIG=""  # Uncomment to disable transform

# Create logs directory if it doesn't exist
mkdir -p "$LOG_DIR"

# Check if service is already running
if [ -f "$PID_FILE" ]; then
    PID=$(cat "$PID_FILE")
    if kill -0 "$PID" 2>/dev/null; then
        echo "ElasticRelay is already running with PID: $PID"
        echo "Use ./stop.sh to stop it first."
        exit 1
    else
        echo "Removing stale PID file..."
        rm -f "$PID_FILE"
    fi
fi

# Check if binary exists
if [ ! -f "$BINARY" ]; then
    echo "Error: ElasticRelay binary not found at $BINARY"
    echo "Please run 'make build' to build the project first."
    exit 1
fi

case "$CONFIG_SOURCE" in
file)
    # Check if config exists
    if [ ! -f "$CONFIG" ]; then
        echo "Error: Configuration file not found at $CONFIG"
        exit 1
    fi

    # Build command arguments
    export ELASTICRELAY_CONFIG_SOURCE=file
    CMD_ARGS="-config $CONFIG -port $PORT"

    # Add transform config if specified and file exists
    if [ -n "$TRANSFORM_CONFIG" ] && [ -f "$TRANSFORM_CONFIG" ]; then
        CMD_ARGS="$CMD_ARGS -transform-config $TRANSFORM_CONFIG"
        echo "Transform Config: $TRANSFORM_CONFIG"
    else
        echo "Transform Config: (none - pass-through mode)"
    fi
    CONFIG_DESC="$CONFIG"
    ;;
nacos)
    if [ -z "$NACOS_SERVER_ADDR" ]; then
        echo "Error: NACOS_SERVER_ADDR is required when CONFIG_SOURCE=nacos"
        exit 1
    fi
    if [ -n "$NACOS_USERNAME" ] && [ -z "$NACOS_PASSWORD" ]; then
        echo "Error: NACOS_PASSWORD is not set. Put NACOS_PASSWORD=xxx in ./.env or export it."
        exit 1
    fi

    # Settings are passed as environment variables so the password never appears in the process list
    export ELASTICRELAY_CONFIG_SOURCE=nacos
    export NACOS_SERVER_ADDR NACOS_NAMESPACE NACOS_GROUP NACOS_DATA_ID NACOS_TRANSFORM_DATA_ID NACOS_USERNAME NACOS_PASSWORD
    CMD_ARGS="-port $PORT"
    CONFIG_DESC="nacos://${NACOS_NAMESPACE:-public}/$NACOS_GROUP/$NACOS_DATA_ID (server=$NACOS_SERVER_ADDR)"
    echo "Transform Config: ${NACOS_TRANSFORM_DATA_ID:-(none - pass-through mode)}"
    ;;
*)
    echo "Error: unknown CONFIG_SOURCE '$CONFIG_SOURCE' (expected: file | nacos)"
    exit 1
    ;;
esac

echo "Starting ElasticRelay service..."
echo "Binary: $BINARY"
echo "Config Source: $CONFIG_SOURCE"
echo "Config: $CONFIG_DESC"
echo "Port: $PORT"
echo "Logs: $LOG_DIR"

# Start the service
if [ "$1" == "-d" ] || [ "$1" == "--daemon" ]; then
    # Run in background
    nohup "$BINARY" $CMD_ARGS > "$LOG_DIR/backend.log" 2>&1 &
    PID=$!
    echo $PID > "$PID_FILE"
    echo "ElasticRelay started in background with PID: $PID"
    echo "Check logs: tail -f $LOG_DIR/backend.log"
else
    # Run in foreground
    echo "Starting in foreground mode (use Ctrl+C to stop)..."
    echo "Use '$0 -d' to start in background mode."
    echo ""
    
    # Start the process in background to get its PID
    "$BINARY" $CMD_ARGS &
    PROCESS_PID=$!
    
    # Write the actual process PID to file
    echo $PROCESS_PID > "$PID_FILE"
    echo "ElasticRelay started in foreground with PID: $PROCESS_PID"
    
    # Set up cleanup trap to remove PID file and kill process on exit
    trap 'echo "Shutting down..."; kill $PROCESS_PID 2>/dev/null; rm -f "$PID_FILE"; exit' EXIT INT TERM
    
    # Wait for the process to finish
    wait $PROCESS_PID
fi