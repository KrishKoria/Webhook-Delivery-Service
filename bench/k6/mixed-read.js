// Admin reads with and without write load. reads_idle runs alone for 30s,
// then reads_loaded runs the same reads while ingest hammers the database.
// Compare http_req_duration{scenario:reads_idle} with {scenario:reads_loaded}.
import http from 'k6/http';
import exec from 'k6/execution';
import { BASE, createSubscriptions, ingest, payload, settle, summary } from './lib.js';

const INGEST_RATE = Number(__ENV.RATE || 300);
const READ_RATE = 20;
const PAGES = ['/ui/dashboard', '/ui/subscriptions', '/api/metrics', '/ui/backlog'];

function readers(startTime, duration) {
  return {
    executor: 'constant-arrival-rate',
    exec: 'read',
    rate: READ_RATE,
    timeUnit: '1s',
    startTime,
    duration,
    preAllocatedVUs: 20,
    maxVUs: 200,
  };
}

export const options = {
  discardResponseBodies: true,
  setupTimeout: '1m',
  teardownTimeout: '1m',
  scenarios: {
    reads_idle: readers('0s', '30s'),
    reads_loaded: readers('30s', '60s'),
    ingest: {
      executor: 'constant-arrival-rate',
      exec: 'write',
      rate: INGEST_RATE,
      timeUnit: '1s',
      startTime: '30s',
      duration: '60s',
      preAllocatedVUs: 100,
      maxVUs: 1000,
    },
  },
  // Not pass/fail; listing them makes k6 report each sub-metric.
  thresholds: Object.assign(
    {
      'http_req_duration{scenario:reads_idle}': ['p(95)<60000'],
      'http_req_duration{scenario:reads_loaded}': ['p(95)<60000'],
    },
    ...PAGES.map((p) => ({
      [`http_req_duration{scenario:reads_idle,name:${p}}`]: ['p(95)<60000'],
      [`http_req_duration{scenario:reads_loaded,name:${p}}`]: ['p(95)<60000'],
    })),
  ),
};

export function setup() {
  // Seed some history so the dashboard queries have rows to aggregate.
  const subs = createSubscriptions(10);
  for (let i = 0; i < 500; i++) ingest(subs[i % subs.length], payload(1024));
  return { subs, start: Date.now() };
}

export function read() {
  const page = PAGES[exec.scenario.iterationInTest % PAGES.length];
  http.get(BASE + page, { tags: { name: page } });
}

export function write(data) {
  ingest(data.subs[exec.scenario.iterationInTest % data.subs.length], payload(1024));
}

export function teardown(data) {
  settle(data.start, 0);
}

export const handleSummary = summary;
