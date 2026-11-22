#!/usr/bin/env bash
# Load-test runner. Each case gets a fresh stack and database:
#   bench/run.sh              # every case
#   bench/run.sh <case>...    # e.g. bench/run.sh idempotency delivery-drain
#   bench/run.sh --list
# Results land in bench/results/<timestamp>/. Scenario knobs (RATE, PEAK_RPS,
# RAMP_SECONDS, DURATION) and stack knobs (BENCH_APP_CPUS, BENCH_APP_MEM,
# BENCH_WORKERS) can be set in the environment.
set -euo pipefail
export MSYS_NO_PATHCONV=1 # stop Git Bash rewriting /scripts/... into Windows paths

cd "$(dirname "$0")"
PORT=${BENCH_PORT:-18080}
PPROF_PORT=${BENCH_PPROF_PORT:-16060}

# case name -> "script [VAR=value ...]"
declare -A CASES=(
  [idempotency]="idempotency"
  [rate-limit]="rate-limit BENCH_RATE_LIMIT=true"
  [payload-size]="payload-size"
  [mixed-read]="mixed-read"
  [delivery-drain]="delivery-drain"
  [delivery-drain-w50]="delivery-drain BENCH_WORKERS=50"
  [delivery-drain-redis]="delivery-drain BENCH_REDIS_URL=redis://redis:6379/0"
  [slow-receivers]="slow-receivers"
  [ingest-ramp]="ingest-ramp"
  [ingest-ramp-redis]="ingest-ramp BENCH_REDIS_URL=redis://redis:6379/0"
)
ORDER=(idempotency rate-limit payload-size mixed-read delivery-drain delivery-drain-w50
  delivery-drain-redis slow-receivers ingest-ramp ingest-ramp-redis)

compose() { docker compose --progress quiet -f compose.yml --profile redis --profile tools "$@"; }

if [[ ${1:-} == --list ]]; then printf '%s\n' "${ORDER[@]}"; exit 0; fi
selected=("${@:-${ORDER[@]}}")
for c in "${selected[@]}"; do
  [[ -n ${CASES[$c]:-} ]] || { echo "unknown case: $c (try --list)" >&2; exit 2; }
done

# On any exit (done, failed or Ctrl-C) remove the stack's containers, network
# and DB volume, plus every image it pulled or built and dangling images left
# by rebuilds. BENCH_KEEP_IMAGES=1 keeps images for back-to-back runs.
cleanup() {
  echo
  if [[ ${BENCH_KEEP_IMAGES:-} == 1 ]]; then
    echo "cleaning up containers and volumes (keeping images)..."
    compose down -v --remove-orphans >/dev/null 2>&1 || true
  else
    echo "cleaning up containers, volumes and images (BENCH_KEEP_IMAGES=1 keeps images)..."
    compose down -v --remove-orphans --rmi all >/dev/null 2>&1 || true
    docker image prune -f --filter "label=com.docker.compose.project=webhook-bench" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

RUN=$(date +%Y%m%d-%H%M%S)
OUT="results/$RUN"
mkdir -p "$OUT"

{
  echo "run: $RUN"
  echo "git: $(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
  echo "docker: $(docker info --format '{{.NCPU}} CPUs, {{.MemTotal}} bytes, {{.OperatingSystem}}')"
  echo "app limits: cpus=${BENCH_APP_CPUS:-1} mem=${BENCH_APP_MEM:-512m} workers=${BENCH_WORKERS:-10} (per case overrides apply)"
  echo "k6: $(compose run --rm --no-deps k6 version 2>/dev/null | head -1)"
} > "$OUT/env.txt"
cat "$OUT/env.txt"

echo "building app image..."
compose build app >/dev/null

wait_ready() {
  for _ in $(seq 1 60); do
    curl -fsS "http://127.0.0.1:$PORT/readyz" >/dev/null 2>&1 && return 0
    sleep 1
  done
  echo "app never became ready" >&2
  compose logs app | tail -30 >&2
  return 1
}

# App CPU/memory until killed. Each docker stats call takes a couple of
# seconds, so this loop runs on its own and samples as fast as it can.
sample_stats() {
  local label=$1 cid
  cid=$(compose ps -q app)
  echo "unix_ms,cpu_pct,mem_usage" > "$OUT/$label-stats.csv"
  while true; do
    echo "$(date +%s%3N),$(docker stats --no-stream --format '{{.CPUPerc}},{{.MemUsage}}' "$cid" 2>/dev/null)" >> "$OUT/$label-stats.csv" || true
  done
}

# Queue counts from /api/metrics once a second until killed.
sample_queue() {
  local label=$1 m
  : > "$OUT/$label-queue.ndjson"
  while true; do
    m=$(curl -fsS --max-time 5 "http://127.0.0.1:$PORT/api/metrics" 2>/dev/null) &&
      echo "{\"unix_ms\":$(date +%s%3N),\"metrics\":$m}" >> "$OUT/$label-queue.ndjson" || true
    sleep 1
  done
}

# CPU, heap and goroutine profiles during steady load (delivery-drain's 60s
# ingest phase). The ramp aborts too early and too abruptly to profile.
profile() {
  local label=$1
  sleep "${PROFILE_AFTER:-20}"
  curl -fsS --max-time 60 "http://127.0.0.1:$PPROF_PORT/debug/pprof/profile?seconds=30" -o "$OUT/$label-cpu.pb.gz" || true
  curl -fsS --max-time 30 "http://127.0.0.1:$PPROF_PORT/debug/pprof/heap" -o "$OUT/$label-heap.pb.gz" || true
  curl -fsS --max-time 30 "http://127.0.0.1:$PPROF_PORT/debug/pprof/goroutine?debug=1" -o "$OUT/$label-goroutines.txt" || true
}

run_case() {
  local label=$1 spec=(${CASES[$1]})
  local script=${spec[0]}
  echo
  echo "=== $label ==="
  (
    # However this subshell exits (including set -e), stop its background
    # helpers; otherwise they outlive the case and keep the terminal busy.
    local bg=()
    trap 'for p in "${bg[@]}"; do kill "$p" 2>/dev/null || true; done; wait 2>/dev/null || true' EXIT

    for kv in "${spec[@]:1}"; do export "${kv?}"; done
    compose down -v --remove-orphans >/dev/null 2>&1 || true
    # Explicit exits: set -e is off here because run_case is called with ||.
    local services=(app)
    [[ -n ${BENCH_REDIS_URL:-} ]] && services=(redis app)
    compose up -d "${services[@]}" >/dev/null || exit 1
    wait_ready || exit 1

    sample_stats "$label" & bg+=($!)
    sample_queue "$label" & bg+=($!)
    if [[ $script == delivery-drain ]]; then profile "$label" & bg+=($!); fi

    local status=0
    compose run --rm --no-deps \
      -e RUN="$RUN" -e LABEL="$label" \
      -e K6_WEB_DASHBOARD_EXPORT="/results/$RUN/$label.html" \
      -e RATE -e PEAK_RPS -e RAMP_SECONDS -e DURATION \
      -e LIMIT_RPS="${BENCH_RATE_LIMIT_RPS:-10}" -e LIMIT_BURST="${BENCH_RATE_LIMIT_BURST:-20}" \
      k6 run --quiet "/scripts/$script.js" || status=$?

    compose logs --no-color app > "$OUT/$label-app.log" 2>&1 || true
    compose down -v --remove-orphans >/dev/null 2>&1 || true
    # k6 exits 99 when a threshold fails; that is a result, not a crash.
    echo "$label $status" >> "$OUT/status.txt"
    echo "=== $label finished (k6 exit $status) ==="
  )
}

# A case that breaks the harness (say, the app never becomes ready) is
# recorded and the rest still run.
for c in "${selected[@]}"; do
  run_case "$c" || echo "$c harness-error" >> "$OUT/status.txt"
done

echo
echo "results: bench/$OUT"
cat "$OUT/status.txt"
