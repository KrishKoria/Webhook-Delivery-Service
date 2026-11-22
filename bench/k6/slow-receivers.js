// Bad receivers: half the subscriptions answer after 200ms, half fail 50% of
// the time. Shows worker saturation (deliveries/s is capped near
// WORKER_CONCURRENCY / receiver latency) and the retry backlog that builds up.
import exec from 'k6/execution';
import { createSubscriptions, ingest, payload, settle, SINKS, summary } from './lib.js';

const RATE = Number(__ENV.RATE || 100);

export const options = {
  discardResponseBodies: true,
  setupTimeout: '1m',
  teardownTimeout: '3m',
  scenarios: {
    ingest: {
      executor: 'constant-arrival-rate',
      rate: RATE,
      timeUnit: '1s',
      duration: '2m',
      preAllocatedVUs: 50,
      maxVUs: 500,
    },
  },
  thresholds: {
    ingest_rejected: ['count==0'],
  },
};

export function setup() {
  return {
    subs: createSubscriptions(5, SINKS.slow).concat(createSubscriptions(5, SINKS.flaky)),
    start: Date.now(),
  };
}

export default function (data) {
  ingest(data.subs[exec.scenario.iterationInTest % data.subs.length], payload(1024));
}

// Retries back off for minutes, so give the queue two minutes and then
// record what is still waiting.
export function teardown(data) {
  settle(data.start, 120);
}

export const handleSummary = summary;
