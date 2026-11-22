// Concurrent duplicates: 200 VUs each send KEYS events, all VUs using the same
// Idempotency-Key for their n-th event. Exactly one request per key may get a
// 202; every other one must be a 200 duplicate, and only KEYS tasks may exist.
import exec from 'k6/execution';
import { createSubscriptions, ingest, payload, settle, summary } from './lib.js';

const VUS = 200;
const KEYS = 20;

export const options = {
  discardResponseBodies: true,
  setupTimeout: '1m',
  teardownTimeout: '1m',
  scenarios: {
    duplicates: { executor: 'per-vu-iterations', vus: VUS, iterations: KEYS, maxDuration: '2m' },
  },
  thresholds: {
    ingest_accepted: [`count==${KEYS}`],
    ingest_duplicate: [`count==${VUS * KEYS - KEYS}`],
    ingest_rejected: ['count==0'],
    queue_total: [`value==${KEYS}`],
  },
};

export function setup() {
  return { sub: createSubscriptions(1)[0], start: Date.now() };
}

export default function (data) {
  const key = `bench-key-${exec.vu.iterationInScenario}`;
  ingest(data.sub, payload(256), { headers: { 'Idempotency-Key': key } });
}

export function teardown(data) {
  settle(data.start, 0);
}

export const handleSummary = summary;
