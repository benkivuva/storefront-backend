import http from 'k6/http';
import { check } from 'k6';
import { Counter } from 'k6/metrics';

// Custom counters so we can report cache hit ratio in the summary
const cacheHits = new Counter('cache_hits');
const cacheMisses = new Counter('cache_misses');

// Warm the cache once before the timed test
export function setup() {
  const res = http.get('http://localhost:8080/api/v1/products?limit=20');
  check(res, { 'setup 200': (r) => r.status === 200 });
  const products = res.json().products;
  return { ids: products.map((p) => p.id) };
}

export const options = {
  scenarios: {
    // 900 req/sec for 30s = 27,000 requests (matches §2 "~900 reads RPS")
    steady: {
      executor: 'constant-arrival-rate',
      rate: 900,
      timeUnit: '1s',
      duration: '30s',
      preAllocatedVUs: 100,
      maxVUs: 300,
    },
  },
  thresholds: {
    // §2: p99 < 50ms
    'http_req_duration{expected_response:true}': ['p(99)<50'],
    // Proxy for cache hit ratio: p50 < 10ms proves most requests are hits
    'http_req_duration{expected_response:true}': ['p(50)<10', 'p(99)<50'],
    // <1% error rate
    'http_req_failed': ['rate<0.01'],
  },
};

export default function (data) {
  const rand = Math.random();
  let res;

  if (rand < 0.6) {
    res = http.get('http://localhost:8080/api/v1/products?limit=20');
  } else if (rand < 0.9) {
    const id = data.ids[Math.floor(Math.random() * data.ids.length)];
    res = http.get(`http://localhost:8080/api/v1/products/${id}`);
  } else {
    res = http.get('http://localhost:8080/api/v1/categories');
  }

  // If response is under 10ms, it almost certainly came from cache
  if (res.timings.duration < 10) {
    cacheHits.add(1);
  } else {
    cacheMisses.add(1);
  }

  check(res, { 'status is 200': (r) => r.status === 200 });
}
