#!/usr/bin/env bash
# Publish a local ElasticRelay configuration file to Nacos (OpenAPI v1).
#
# Usage:
#   scripts/nacos_publish.sh <config-file> [data-id]
#
# Environment variables (defaults in brackets):
#   NACOS_SERVER_ADDR  [127.0.0.1:8848]  first address is used
#   NACOS_NAMESPACE    []                namespace ID, empty means public
#   NACOS_GROUP        [ELASTICRELAY]
#   NACOS_DATA_ID      [elasticrelay.json]  used when data-id is not given
#   NACOS_USERNAME / NACOS_PASSWORD     optional, for servers with auth enabled
#   NACOS_CONTEXT_PATH [/nacos]

set -euo pipefail

if [[ $# -lt 1 ]]; then
  sed -n '2,15p' "$0"
  exit 1
fi

FILE="$1"
DATA_ID="${2:-${NACOS_DATA_ID:-elasticrelay.json}}"
ADDR="${NACOS_SERVER_ADDR:-127.0.0.1:8848}"
ADDR="${ADDR%%,*}"
[[ "$ADDR" == http://* || "$ADDR" == https://* ]] || ADDR="http://$ADDR"
BASE="${ADDR%/}${NACOS_CONTEXT_PATH:-/nacos}"
GROUP="${NACOS_GROUP:-ELASTICRELAY}"
NAMESPACE="${NACOS_NAMESPACE:-}"

if [[ ! -f "$FILE" ]]; then
  echo "File not found: $FILE" >&2
  exit 1
fi

if command -v python3 >/dev/null 2>&1; then
  python3 -m json.tool "$FILE" >/dev/null || { echo "Invalid JSON: $FILE" >&2; exit 1; }
fi

TOKEN_PARAM=()
if [[ -n "${NACOS_USERNAME:-}" ]]; then
  LOGIN=$(curl -sS -X POST "$BASE/v1/auth/login" \
    --data-urlencode "username=$NACOS_USERNAME" \
    --data-urlencode "password=${NACOS_PASSWORD:-}")
  TOKEN=$(printf '%s' "$LOGIN" | sed -n 's/.*"accessToken"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
  if [[ -z "$TOKEN" ]]; then
    echo "Nacos login failed: $LOGIN" >&2
    exit 1
  fi
  TOKEN_PARAM=(--data-urlencode "accessToken=$TOKEN")
fi

RESULT=$(curl -sS -X POST "$BASE/v1/cs/configs" \
  --data-urlencode "dataId=$DATA_ID" \
  --data-urlencode "group=$GROUP" \
  --data-urlencode "tenant=$NAMESPACE" \
  --data-urlencode "type=json" \
  --data-urlencode "content@$FILE" \
  ${TOKEN_PARAM[@]+"${TOKEN_PARAM[@]}"})

if [[ "$RESULT" == "true" ]]; then
  echo "Published $FILE to nacos://${NAMESPACE:-public}/$GROUP/$DATA_ID"
else
  echo "Publish failed: $RESULT" >&2
  exit 1
fi
