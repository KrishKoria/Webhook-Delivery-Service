# Load tests

Push the service to its limits on your own machine. Everything runs in Docker
([k6](https://grafana.com/docs/k6/) for load,
[go-httpbin](https://github.com/mccutchen/go-httpbin) as the webhook receiver),
so the only requirement is Docker with Compose.

```bash
bench/run.sh               # every case, about 40 minutes
bench/run.sh --list        # case names
bench/run.sh delivery-drain idempotency
```

On Windows, run it from Git Bash.

Each case starts a fresh stack with an empty SQLite database on a Docker
volume, runs one k6 script, and tears everything down. While a case runs, the
admin UI is at <http://127.0.0.1:18080> if you want to watch the queue move.

## The stack

| Service | What it is |
|---|---|
| `app` | The service, built from the repo `Dockerfile`. Capped at **1 CPU and 512 MB** by default (a default Cloud Run instance), so results read as "what one instance can take". |
| `sink` | go-httpbin as the webhook target: `/status/204` (fast), `/delay/200ms` (slow), `/status/200:0.5,500:0.5` (flaky). |
| `redis` | Only for the `*-redis` cases. |
| `k6` | The load generator. Not CPU-capped, so it shouldn't be the bottleneck. |

## Cases

| Case | What it does | What it answers |
|---|---|---|
| `idempotency` | 200 VUs send 20 rounds, all VUs sharing one `Idempotency-Key` per round | Exactly one 202 per key under a concurrent race |
| `rate-limit` | 50 rps to each of 2 subscriptions with the limiter on (10 rps, burst 20) | Admits about burst + rate × seconds per subscription; every rejection is a 429 with `Retry-After` |
| `payload-size` | 1 KB, 64 KB, then 1 MB bodies, then a 1 MB + 1 byte body | Throughput by body size; the 413 cap holds |
| `mixed-read` | Admin pages alone for 30s, then the same pages while ingest runs at 300 rps | How much UI and API reads slow down under write load |
| `delivery-drain` | 300 rps of ingest for 60s, then wait until everything is delivered | Delivery throughput, drain time, and a reconciliation: every 202 became a delivered task |
| `delivery-drain-w50` | Same, with `WORKER_CONCURRENCY=50` | Whether more workers help |
| `delivery-drain-redis` | Same, with the Redis subscription cache | Whether the cache helps |
| `slow-receivers` | 100 rps to receivers that are slow or fail 50% of the time | Worker saturation and the retry backlog |
| `ingest-ramp` | Ramp from 50 to 5,000 rps over 3 minutes; abort when errors pass 1% or p99 passes 500 ms | The breaking point (`offered_rps` max) |
| `ingest-ramp-redis` | Same, with Redis | Breaking point with the cache |

## Knobs

Set these in the environment of `bench/run.sh`:

| Variable | Default | Effect |
|---|---|---|
| `BENCH_APP_CPUS` / `BENCH_APP_MEM` | `1` / `512m` | App container limits |
| `BENCH_WORKERS` | `10` | `WORKER_CONCURRENCY` |
| `RATE` | per script | Ingest rate for constant-rate cases |
| `DURATION` | `60s` | `delivery-drain` ingest phase |
| `PEAK_RPS` / `RAMP_SECONDS` | `5000` / `180` | `ingest-ramp` target and ramp length |
| `PROFILE_AFTER` | `20` | Seconds into a `delivery-drain` case before the 30s CPU profile starts |
| `BENCH_PORT` / `BENCH_PPROF_PORT` | `18080` / `16060` | Host ports (bound to 127.0.0.1) |
| `BENCH_KEEP_IMAGES` | unset | When the script exits (including Ctrl-C) it removes the stack's containers, volume and images. Set to `1` to keep the images and skip about 250 MB of downloads on the next run. |

## Results

`bench/results/<timestamp>/` (gitignored):

| File | Contents |
|---|---|
| `<case>.html` | k6's HTML report with charts. Open it in a browser. Very short cases (like `idempotency`) skip it: k6 needs at least three 2-second periods of data. |
| `<case>.json` | k6's summary data plus a `reconciliation` block (accepted, tasks, delivered, failed, `lost`) |
| `<case>-queue.ndjson` | `/api/metrics` once a second: the queue's backlog and delivered count over time |
| `<case>-stats.csv` | App container CPU and memory over time |
| `<case>-cpu.pb.gz`, `-heap.pb.gz`, `-goroutines.txt` | Profiles from the `delivery-drain` cases under steady load. Read them with `go tool pprof -top bench/results/<run>/delivery-drain-cpu.pb.gz` |
| `<case>-app.log` | App logs (level WARN and above) |
| `status.txt` | k6 exit code per case. `99` means a threshold failed, which is a result rather than a crash. |
| `env.txt` | Git commit, Docker resources and k6 version |

## Caveats

- The load generator, the receiver and the app share one machine. The app's CPU cap keeps it from borrowing k6's cores, but this is still not a lab.
- Docker Desktop on Windows and macOS runs containers in a VM, and its disk I/O is slower than native Linux. Compare runs on the same machine.
- The queue sampler calls `/api/metrics` once a second, which adds a little read load of its own.
