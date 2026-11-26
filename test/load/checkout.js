import http from 'k6/http';
import { check } from 'k6';
import { Counter } from 'k6/metrics';

// Checkout load scenario. Run against a staging database seeded with a product
// that has enough stock, and pass a real customer token + address:
//
//   BASE_URL=https://api.staging.example.com \
//   ACCESS_TOKEN=... ADDRESS_ID=... PRODUCT_ID=... \
//   k6 run test/load/checkout.js
//
// Each iteration adds one unit to the cart and checks out with a unique
// idempotency key, so the scenario measures the full checkout transaction.

const created = new Counter('checkout_created');
const conflicts = new Counter('checkout_conflicts');

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const TOKEN = __ENV.ACCESS_TOKEN;
const ADDRESS_ID = __ENV.ADDRESS_ID;
const PRODUCT_ID = __ENV.PRODUCT_ID;
const EXPECTED_TOTAL = Number(__ENV.EXPECTED_TOTAL_MINOR || 60000);

export const options = {
  scenarios: {
    steady: {
      executor: 'constant-arrival-rate',
      rate: Number(__ENV.RATE || 50),
      timeUnit: '1s',
      duration: __ENV.DURATION || '30s',
      preAllocatedVUs: 50,
      maxVUs: 200,
    },
  },
  thresholds: {
    'http_req_duration{expected_response:true}': ['p(99)<250'],
    'http_req_failed': ['rate<0.01'],
  },
};

export function setup() {
  if (!TOKEN || !ADDRESS_ID || !PRODUCT_ID) {
    throw new Error('ACCESS_TOKEN, ADDRESS_ID and PRODUCT_ID are required');
  }
}

export default function () {
  const headers = {
    Authorization: `Bearer ${TOKEN}`,
    'Content-Type': 'application/json',
  };

  const add = http.post(
    `${BASE_URL}/api/v1/cart/items`,
    JSON.stringify({ product_id: PRODUCT_ID, quantity: 1 }),
    { headers },
  );
  check(add, { 'cart add 204': (r) => r.status === 204 });

  const idem = `k6-${__VU}-${__ITER}-${Date.now()}`;
  const res = http.post(
    `${BASE_URL}/api/v1/checkout`,
    JSON.stringify({ address_id: ADDRESS_ID, expected_total_minor: EXPECTED_TOTAL }),
    { headers: { ...headers, 'Idempotency-Key': idem } },
  );

  if (res.status === 201) {
    created.add(1);
  } else if (res.status === 409) {
    conflicts.add(1);
  }

  check(res, { 'checkout 201 or 409': (r) => r.status === 201 || r.status === 409 });
}
