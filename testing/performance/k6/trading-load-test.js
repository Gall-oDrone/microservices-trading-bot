import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate } from 'k6/metrics';

// Custom metrics
const errorRate = new Rate('errors');

export const options = {
  stages: [
    { duration: '2m', target: 10 }, // Ramp up to 10 users
    { duration: '5m', target: 10 }, // Stay at 10 users
    { duration: '2m', target: 20 }, // Ramp up to 20 users
    { duration: '5m', target: 20 }, // Stay at 20 users
    { duration: '2m', target: 0 },  // Ramp down to 0 users
  ],
  thresholds: {
    http_req_duration: ['p(95)<2000'], // 95% of requests must complete below 2s
    http_req_failed: ['rate<0.1'],     // Error rate must be below 10%
    errors: ['rate<0.1'],              // Custom error rate must be below 10%
  },
};

const BASE_URL = 'http://api-gateway:8085';

export default function () {
  // Test API Gateway health
  const healthResponse = http.get(`${BASE_URL}/health`);
  check(healthResponse, {
    'health check status is 200': (r) => r.status === 200,
    'health check response time < 500ms': (r) => r.timings.duration < 500,
  });
  errorRate.add(healthResponse.status !== 200);

  // Test market data endpoint
  const marketDataResponse = http.get(`${BASE_URL}/api/v1/market-data/btc-mxn`);
  check(marketDataResponse, {
    'market data status is 200': (r) => r.status === 200,
    'market data response time < 1000ms': (r) => r.timings.duration < 1000,
    'market data has required fields': (r) => {
      const data = JSON.parse(r.body);
      return data.hasOwnProperty('bid') && data.hasOwnProperty('ask');
    },
  });
  errorRate.add(marketDataResponse.status !== 200);

  // Test order placement (simulated)
  const orderPayload = JSON.stringify({
    side: 'buy',
    book: 'btc_mxn',
    amount: '0.001',
    price: '500000',
  });
  
  const orderResponse = http.post(`${BASE_URL}/api/v1/orders`, orderPayload, {
    headers: { 'Content-Type': 'application/json' },
  });
  check(orderResponse, {
    'order placement status is 200 or 400': (r) => r.status === 200 || r.status === 400,
    'order placement response time < 2000ms': (r) => r.timings.duration < 2000,
  });
  errorRate.add(orderResponse.status >= 500);

  sleep(1);
}
