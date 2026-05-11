#!/usr/bin/env bash
set -euo pipefail

# Launch all A2A agents for the agentigslide pipeline.
#
# Background agents: outliner (:8080), selector (:8081), writer (:8082), reviewer (:8083)
# Foreground: orchestrator (:8084) — Ctrl+C stops everything.
#
# Usage:
#   ./exp/scripts/agents.sh
#
# Prerequisites:
#   - VERTEX_PROJECT_ID must be set
#   - SLIDES_TEMPLATE_ID must be set (for orchestrator)
#   - Template index must be built (go run buildTemplateIndex/build_template_index.go)

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
BINDIR="$ROOT_DIR/bin"

echo "Building agents..."
make -C "$ROOT_DIR" agents
echo ""

PIDS=()

cleanup() {
    echo ""
    echo "Stopping agents..."
    for pid in "${PIDS[@]}"; do
        kill "$pid" 2>/dev/null || true
    done
    wait 2>/dev/null
    echo "All agents stopped."
}

trap cleanup EXIT INT TERM

echo "Starting outliner on :8080..."
"$BINDIR/agent_outliner" --addr :8080 &
PIDS+=($!)

echo "Starting selector on :8081..."
"$BINDIR/agent_selector" --addr :8081 &
PIDS+=($!)

echo "Starting writer on :8082..."
"$BINDIR/agent_writer" --addr :8082 &
PIDS+=($!)

echo "Starting reviewer on :8083..."
"$BINDIR/agent_reviewer" --addr :8083 &
PIDS+=($!)

echo ""
echo "All background agents started. Starting orchestrator on :8084 (foreground)..."
echo "Press Ctrl+C to stop all agents."
echo ""

"$BINDIR/agent_orchestrator" --addr :8084
