// Breaking point: ramp the ingest rate until errors or p99 latency blow up.
// The delivery worker runs against a fast sink the whole time, so this is
// ingest under realistic write contention, not ingest in isolation.
import exec from 'k6/execution';
import { Gauge } from 'k6/metrics';
import { createSubscriptions, ingest, payload, settle, summary } from './lib.js';

const START = 50;
const PEAK = Number(__ENV.PEAK_RPS || 5000);
const RAMP_SECONDS = Number(__ENV.RAMP_SECONDS || 180);

// The offered rate when each request started; its max is the highest rate
// reached before an abort.
const offered = new Gauge('offered_rps');

export const options = {
  discardResponseBodies: true,
  setupTimeout: '1m',
  teardownTimeout: '1m',
  scenarios: {
    ingest: {
      executor: 'ramping-arrival-rate',
      startRate: START,
      timeUnit: '1s',
      preAllocatedVUs: 200,
      maxVUs: 2000,
      stages: [{ target: PEAK, duration: `${RAMP_SECONDS}s` }],
    },
  },
  thresholds: {
    'http_req_failed{name:ingest}': [{ threshold: 'rate<0.01', abortOnFail: true, delayAbortEval: '10s' }],
    'http_req_duration{name:ingest}': [{ threshold: 'p(99)<500', abortOnFail: true, delayAbortEval: '10s' }],
  },
};

export function setup() {
  return { subs: createSubscriptions(20), start: Date.now() };
}

export default function (data) {
  const elapsed = (Date.now() - exec.scenario.startTime) / 1000;
  offered.add(START + (PEAK - START) * Math.min(1, elapsed / RAMP_SECONDS));
  ingest(data.subs[exec.scenario.iterationInTest % data.subs.length], payload(1024));
}

// No drain: at the breaking point the backlog can take far longer to clear
// than the test ran. Record where the queue stands instead.
export function teardown(data) {
  settle(data.start, 0);
}

export const handleSummary = summary;
