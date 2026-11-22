// End to end: ingest at a steady rate, then wait for every task to reach the
// sink. Reports delivery throughput and drain time, and checks that every
// 202 became a delivered task (reconciliation.lost == 0, nothing stuck).
import exec from 'k6/execution';
import { createSubscriptions, ingest, payload, settle, summary } from './lib.js';

const RATE = Number(__ENV.RATE || 300);
const DURATION = __ENV.DURATION || '60s';

export const options = {
  discardResponseBodies: true,
  setupTimeout: '1m',
  teardownTimeout: '11m',
  scenarios: {
    ingest: {
      executor: 'constant-arrival-rate',
      rate: RATE,
      timeUnit: '1s',
      duration: DURATION,
      preAllocatedVUs: 100,
      maxVUs: 1000,
    },
  },
  thresholds: {
    ingest_rejected: ['count==0'],
    queue_failed: ['value==0'],
    checks: ['rate==1'], // includes "queue drained"
  },
};

export function setup() {
  return { subs: createSubscriptions(10), start: Date.now() };
}

export default function (data) {
  ingest(data.subs[exec.scenario.iterationInTest % data.subs.length], payload(1024));
}

export function teardown(data) {
  settle(data.start, 600);
}

export const handleSummary = summary;
