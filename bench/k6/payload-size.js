// Body size: 1 KB, 64 KB and 1 MB events, one after another, plus a check
// that a body one byte over INGEST_MAX_BODY_SIZE (1 MiB) gets 413.
import exec from 'k6/execution';
import { check } from 'k6';
import { createSubscriptions, ingest, payload, settle, summary } from './lib.js';

const LIMIT = 1048576;
const SIZES = { kb1: 1024, kb64: 65536, mb1: LIMIT };

function stage(rate, startTime) {
  return {
    executor: 'constant-arrival-rate',
    exec: 'send',
    rate,
    timeUnit: '1s',
    startTime,
    duration: '30s',
    preAllocatedVUs: 50,
    maxVUs: 500,
  };
}

export const options = {
  discardResponseBodies: true,
  setupTimeout: '1m',
  teardownTimeout: '1m',
  scenarios: {
    kb1: stage(200, '0s'),
    kb64: stage(100, '35s'),
    mb1: stage(10, '70s'),
    over_limit: { executor: 'shared-iterations', exec: 'overLimit', vus: 1, iterations: 5, startTime: '105s' },
  },
  thresholds: Object.assign(
    { checks: ['rate==1'] },
    ...Object.keys(SIZES).map((s) => ({
      [`http_req_duration{scenario:${s}}`]: ['p(95)<60000'], // reported, not judged
      [`http_req_failed{scenario:${s}}`]: ['rate<0.01'],
      [`data_sent{scenario:${s}}`]: ['count>0'],
    })),
  ),
};

export function setup() {
  return { subs: createSubscriptions(10), start: Date.now() };
}

export function send(data) {
  ingest(data.subs[exec.scenario.iterationInTest % data.subs.length], payload(SIZES[exec.scenario.name]));
}

export function overLimit(data) {
  const res = ingest(data.subs[0], payload(LIMIT + 1), { expected: [413] });
  check(res, { 'over-limit body gets 413': (r) => r.status === 413 });
}

export function teardown(data) {
  settle(data.start, 0);
}

export const handleSummary = summary;
