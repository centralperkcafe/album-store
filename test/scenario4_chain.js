// scenario4_chain.js
import http from 'k6/http';
import { check, sleep } from 'k6';

const BASE_URL = 'http://localhost';
const ALBUM_SERVICE = `${BASE_URL}:8080/api/albums`;
const ORDER_SERVICE = `${BASE_URL}:8082/api/orders`;
const INVENTORY_SERVICE = `${BASE_URL}:8081/api/inventory`;

const ADMIN_HEADERS = {
  headers: {
    'Content-Type': 'application/json',
    'Client-Type': 'admin',
  },
};

const USER_HEADERS = {
  headers: {
    'Content-Type': 'application/json',
    'Client-Type': 'user',
  },
};

export const options = {
  vus: 1,
  iterations: 1,
};

export default function () {
  // --- Step 1: Create album ---
  const albumPayload = JSON.stringify({
    title: "Trace Test Album",
    artist: "Tracer",
    price: 9.99,
    releaseYear: 2024,
    genre: "Observability",
    initialQuantity: 20,
  });

  const albumRes = http.post(ALBUM_SERVICE, albumPayload, ADMIN_HEADERS);
  check(albumRes, {
    'Album created': (r) => r.status === 201,
  });

  const albumId = JSON.parse(albumRes.body).id;
  console.log(`🎵 Created albumId: ${albumId}`);
  sleep(1);

  // --- Step 2: Place an order ---
  const orderPayload = JSON.stringify({
    albumId: albumId.toString(),
    quantity: 1,
    userId: "u-trace",
  });

  const orderRes = http.post(ORDER_SERVICE, orderPayload, USER_HEADERS);
  check(orderRes, {
    'Order placed': (r) => r.status === 201,
  });

  const orderId = JSON.parse(orderRes.body).id;
  console.log(`🧾 Created orderId: ${orderId}`);
  sleep(1);

  // --- Step 3: Query inventory ---
  const inventoryRes = http.get(`${INVENTORY_SERVICE}/${albumId}`);
  check(inventoryRes, {
    'Inventory retrieved': (r) => r.status === 200,
  });

  console.log(`📦 Inventory after order: ${inventoryRes.body}`);
}
