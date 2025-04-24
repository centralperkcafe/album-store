// scenario3_concurrency.js
import http from "k6/http";
import { check, sleep } from "k6";

const BASE_URL = "http://localhost";
const ALBUM_SERVICE = `${BASE_URL}:8080/api/albums`;
const ORDER_SERVICE = `${BASE_URL}:8082/api/orders`;

const ADMIN_HEADERS = {
  headers: {
    "Content-Type": "application/json",
    "Client-Type": "admin",
  },
};

const USER_HEADERS = {
  headers: {
    "Content-Type": "application/json",
    "Client-Type": "user",
  },
};

export const options = {
  vus: 5,
  duration: "30s",
};

export function setup() {
  const albumPayload = JSON.stringify({
    title: "Concurrency Album",
    artist: "Speedy",
    price: 9.99,
    releaseYear: 2024,
    genre: "Load",
    initialQuantity: 100,
  });

  const res = http.post(ALBUM_SERVICE, albumPayload, ADMIN_HEADERS);
  const albumId = JSON.parse(res.body).id;
  console.log(`🎵 Created albumId: ${albumId}`);
  return { albumId };
}

export default function (data) {
  const payload = JSON.stringify({
    albumId: data.albumId.toString(),
    quantity: 1,
    userId: `u-${__VU}`,
  });

  const res = http.post(ORDER_SERVICE, payload, USER_HEADERS);

  check(res, {
    "Order created successfully (201)": (r) => r.status === 201,
  });

  sleep(1);
}
