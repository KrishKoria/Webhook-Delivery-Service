// Shared helpers for the k6 scenarios. They run in the k6 container on the
// compose network: BASE_URL is the app, SINK_URL the go-httpbin receiver.
import http from 'k6/http';
import { check, fail, sleep } from 'k6';
import { Counter, Gauge } from 'k6/metrics';
import { textSummary } from 'https://jslib.k6.io/k6-summary/0.1.0/index.js';

export const BASE = __ENV.BASE_URL || 'http://localhost:18080';
const SINK = __ENV.SINK_URL || 'http://sink:8080';

// go-httpbin paths used as webhook targets.
export const SINKS = {
  fast: '/status/204',
  slow: '/delay/200ms',
  flaky: '/status/200:0.5,500:0.5',
};

const JSON_HEADERS = { 'Content-Type': 'application/json' };

// Ingest outcomes by class, so the summary can reconcile them with the queue.
export const accepted = new Counter('ingest_accepted'); // 202 queued
export const duplicates = new Counter('ingest_duplicate'); // 200 repeated Idempotency-Key
export const rejected = new Counter('ingest_rejected'); // anything else

// Queue state recorded once in teardown (see settle).
const queueTotal = new Gauge('queue_total');
const queueDelivered = new Gauge('queue_delivered');
const queueFailed = new Gauge('queue_failed');
const queueActive = new Gauge('queue_active');
const dlqDead = new Gauge('dlq_dead');
const drainSeconds = new Gauge('drain_seconds');
const deliveryRate = new Gauge('delivery_rate_per_s');
const avgDeliveryLatency = new Gauge('delivery_latency_avg_ms');

// createSubscriptions makes n subscriptions targeting the given sink path.
// Admin calls ask for a text body explicitly because scenarios usually set
// discardResponseBodies.
export function createSubscriptions(n, sinkPath = SINKS.fast) {
  const ids = [];
  for (let i = 0; i < n; i++) {
    const res = http.post(`${BASE}/api/subscriptions`, JSON.stringify({ target_url: SINK + sinkPath }), {
      headers: JSON_HEADERS,
      responseType: 'text',
      tags: { name: 'admin' },
    });
    if (res.status !== 201) fail(`create subscription: HTTP ${res.status} ${res.body}`);
    ids.push(res.json('data.id'));
  }
  return ids;
}

// ingest posts one event and counts the outcome.
export function ingest(subID, body, { headers = {}, expected = [200, 202] } = {}) {
  const res = http.post(`${BASE}/ingest/${subID}`, body, {
    headers: Object.assign({}, JSON_HEADERS, headers),
    tags: { name: 'ingest' },
    responseCallback: http.expectedStatuses(...expected),
  });
  if (res.status === 202) accepted.add(1);
  else if (res.status === 200) duplicates.add(1);
  else rejected.add(1, { status: String(res.status) });
  return res;
}

// payload returns a JSON body of exactly `bytes` bytes (minimum ~40).
const payloadCache = {};
export function payload(bytes) {
  if (!payloadCache[bytes]) {
    const shell = JSON.stringify({ event: 'bench', pad: '' });
    payloadCache[bytes] = JSON.stringify({ event: 'bench', pad: 'x'.repeat(Math.max(0, bytes - shell.length)) });
  }
  return payloadCache[bytes];
}

export function metrics() {
  const res = http.get(`${BASE}/api/metrics`, { responseType: 'text', tags: { name: 'admin' } });
  return res.status === 200 ? res.json('data') : null;
}

// settle runs in teardown. With waitSeconds > 0 it waits for the queue to
// drain (nothing pending or in flight) and records how long that took; with 0
// it snapshots the queue as it is. Either way it records the queue state and
// the end-to-end delivery rate since startMs (taken in setup).
export function settle(startMs, waitSeconds) {
  const drainStart = Date.now();
  let m = metrics();
  while (waitSeconds > 0 && m && m.queue.active > 0 && Date.now() - drainStart < waitSeconds * 1000) {
    sleep(0.5);
    m = metrics() || m;
  }
  if (!m) fail('could not read /api/metrics');
  const now = Date.now();
  if (waitSeconds > 0) {
    drainSeconds.add((now - drainStart) / 1000);
    check(m, { 'queue drained': (x) => x.queue.active === 0 });
  }
  queueTotal.add(m.queue.total);
  queueDelivered.add(m.queue.delivered);
  queueFailed.add(m.queue.failed);
  queueActive.add(m.queue.active);
  dlqDead.add(m.dlq ? m.dlq.dead : 0);
  deliveryRate.add(m.queue.delivered / ((now - startMs) / 1000));
  if (m.latency) avgDeliveryLatency.add(m.latency.average_ms);
  return m;
}

function metricValue(data, name, field) {
  const m = data.metrics[name];
  return m ? m.values[field] : 0;
}

// summary prints the usual text summary and writes the full data plus a
// reconciliation block to /results/<RUN>/<LABEL>.json. "lost" counts 202s
// that never became a task row; it must be 0.
export function summary(data) {
  const acceptedCount = metricValue(data, 'ingest_accepted', 'count');
  data.reconciliation = {
    accepted: acceptedCount,
    duplicates: metricValue(data, 'ingest_duplicate', 'count'),
    rejected: metricValue(data, 'ingest_rejected', 'count'),
    tasks: metricValue(data, 'queue_total', 'value'),
    delivered: metricValue(data, 'queue_delivered', 'value'),
    failed: metricValue(data, 'queue_failed', 'value'),
    still_active: metricValue(data, 'queue_active', 'value'),
    lost: acceptedCount - metricValue(data, 'queue_total', 'value'),
  };
  const out = `/results/${__ENV.RUN || 'adhoc'}/${__ENV.LABEL || 'run'}.json`;
  return {
    stdout: textSummary(data, { indent: ' ', enableColors: true }) +
      `\n reconciliation: ${JSON.stringify(data.reconciliation)}\n`,
    [out]: JSON.stringify(data, null, 1),
  };
}
