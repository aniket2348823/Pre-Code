import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';

const errorRate = new Rate('errors');
const latency = new Trend('latency');

export const options = {
  stages: [
    { duration: '30s', target: 50 },   // Ramp up
    { duration: '1m', target: 100 },   // Stay at 100
    { duration: '30s', target: 200 },  // Spike to 200
    { duration: '1m', target: 100 },   // Back to 100
    { duration: '30s', target: 0 },    // Ramp down
  ],
  thresholds: {
    http_req_duration: ['p(95)<500', 'p(99)<1000'],
    errors: ['rate<0.01'],
  },
};

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const API_TOKEN = __ENV.API_TOKEN || '';

const headers = {
  'Content-Type': 'application/json',
  'Authorization': `Bearer ${API_TOKEN}`,
};

export default function () {
  // Test 1: Health check
  let res = http.get(`${BASE_URL}/api/v1/health`);
  check(res, {
    'health status 200': (r) => r.status === 200,
  });
  errorRate.add(res.status !== 200);
  latency.add(res.timings.duration);

  sleep(1);

  // Test 2: Scan endpoint
  res = http.post(`${BASE_URL}/api/v1/scan`, JSON.stringify({
    language: 'go',
    filename: 'test.go',
    code: 'func main() { fmt.Sprintf("SELECT * FROM users WHERE id=%s", id) }',
  }), { headers });
  check(res, {
    'scan status 200': (r) => r.status === 200,
    'scan has findings': (r) => JSON.parse(r.body).findings !== undefined,
  });
  errorRate.add(res.status !== 200);
  latency.add(res.timings.duration);

  sleep(1);

  // Test 3: List skills
  res = http.get(`${BASE_URL}/api/v1/skills?page=1&page_size=20`, { headers });
  check(res, {
    'skills status 200': (r) => r.status === 200,
  });
  errorRate.add(res.status !== 200);
  latency.add(res.timings.duration);

  sleep(1);

  // Test 4: Pipeline validation
  res = http.post(`${BASE_URL}/api/v1/validate-full`, JSON.stringify({
    description: 'Create a secure payment system',
    code: 'func processPayment(amount float64) error { return nil }',
    language: 'go',
  }), { headers });
  check(res, {
    'pipeline status 200': (r) => r.status === 200,
  });
  errorRate.add(res.status !== 200);
  latency.add(res.timings.duration);

  sleep(1);
}

export function handleSummary(data) {
  return {
    'stdout': JSON.stringify(data, null, 2),
    'reports/loadtest-summary.json': JSON.stringify(data, null, 2),
  };
}
