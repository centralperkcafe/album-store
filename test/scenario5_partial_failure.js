// scenario5_partial_failure.js
import http from 'k6/http';
import { check, sleep } from 'k6';

const BASE_URL = 'http://localhost';
const ALBUM_SERVICE = `${BASE_URL}:8080/api/albums`;
const ORDER_SERVICE = `${BASE_URL}:8082/api/orders`;

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
  vus: 10,
  iterations: 10,
};

let sharedAlbumId;

export function setup() {
  const albumPayload = JSON.stringify({
    title: "Limited Edition",
    artist: "Concurrency",
    price: 9.99,
    releaseYear: 2024,
    genre: "Stress",
    initialQuantity: 3,
  });

  const albumRes = http.post(ALBUM_SERVICE, albumPayload, ADMIN_HEADERS);
  const albumId = JSON.parse(albumRes.body).id;
  console.log(`🚀 Created limited albumId: ${albumId}`);
  sharedAlbumId = albumId;

  sleep(1);
  return { albumId };
}

export default function (data) {
  const orderPayload = JSON.stringify({
    albumId: data.albumId.toString(),
    quantity: 1,
    userId: `u-${__VU}`,  
  });

  const orderRes = http.post(ORDER_SERVICE, orderPayload, USER_HEADERS);

  check(orderRes, {
    'Got status 201 or 4xx (some may fail)': (r) =>
      r.status === 201 || (r.status >= 400 && r.status < 500),
  });

  console.log(`🧾 VU${__VU} order status: ${orderRes.status}`);
  sleep(1);
}
