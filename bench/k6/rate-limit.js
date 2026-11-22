// Rate limiter (run with BENCH_RATE_LIMIT=true, 10 rps, burst 20 per
// subscription). Two subscriptions each get 50 rps for 30s; each should be
// admitted about burst + rps * 30 = 320 times, independently, and every
// rejection must be a 429 with Retry-After.
import exec from 'k6/execution';
import { check } from 'k6';
import { Counter } from 'k6/metrics';
import { createSubscriptions, ingest, payload, settle, summary } from './lib.js';

const SECONDS = 30;
const RPS = Number(__ENV.LIMIT_RPS || 10);
const BURST = Number(__ENV.LIMIT_BURST || 20);
const SUBS = 2;
const expectedPerSub = BURST + RPS * SECONDS;

const limited = new Counter('ingest_rate_limited');

export const options = {
  discardResponseBodies: true,
  setupTimeout: '1m',
  teardownTimeout: '1m',
  scenarios: {
    flood: {
      executor: 'constant-arrival-rate',
      rate: 50 * SUBS,
      timeUnit: '1s',
      duration: `${SECONDS}s`,
      preAllocatedVUs: 50,
      maxVUs: 200,
    },
  },
  thresholds: {
    // ±10% of the token-bucket prediction, across both subscriptions.
    ingest_accepted: [
      `count>=${Math.floor(expectedPerSub * SUBS * 0.9)}`,
      `count<=${Math.ceil(expectedPerSub * SUBS * 1.1)}`,
    ],
    checks: ['rate==1'],
  },
};

export function setup() {
  return { subs: createSubscriptions(SUBS), start: Date.now() };
}

export default function (data) {
  const res = ingest(data.subs[exec.scenario.iterationInTest % SUBS], payload(256), { expected: [202, 429] });
  if (res.status === 429) {
    limited.add(1);
    check(res, { '429 carries Retry-After': (r) => Number(r.headers['Retry-After']) >= 1 });
  } else {
    check(res, { 'not limited means 202': (r) => r.status === 202 });
  }
}

export function teardown(data) {
  settle(data.start, 0);
}

export const handleSummary = summary;
