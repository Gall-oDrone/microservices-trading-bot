import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  stages: [
    { duration: '30s', target: 10 },  // Ramp up to 10 users
    { duration: '1m', target: 10 },   // Stay at 10 users
    { duration: '30s', target: 20 },  // Ramp up to 20 users
    { duration: '1m', target: 20 },   // Stay at 20 users
    { duration: '30s', target: 0 },   // Ramp down
  ],
  thresholds: {
    http_req_duration: ['p(95)<500'],  // 95% of requests under 500ms
    http_req_failed: ['rate<0.01'],    // Less than 1% failures
  },
};

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8085';

export default function () {
  // Health check
  let healthRes = http.get(`${BASE_URL}/health`);
  check(healthRes, {
    'health status is 200': (r) => r.status === 200,
  });

  // Get status
  let statusRes = http.get(`${BASE_URL}/api/v1/status`);
  check(statusRes, {
    'status is 200': (r) => r.status === 200,
  });

  // Get strategies
  let strategyRes = http.get(`${BASE_URL}/api/v1/strategies`);
  check(strategyRes, {
    'strategies status is 200': (r) => r.status === 200,
  });

  sleep(1);
}
