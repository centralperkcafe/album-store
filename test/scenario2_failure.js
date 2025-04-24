// scenario2_failure.js
import http from "k6/http";
import { check, sleep } from "k6";

const BASE_URL = "http://localhost";
const ALBUM_SERVICE_URL = `${BASE_URL}:8080/api/albums`;
const ORDER_SERVICE_URL = `${BASE_URL}:8082/api/orders`;
const USER_ID = "u-failure";

export const options = {
  vus: 1,
  iterations: 1,
};

export default function () {
  // Step 1: Create album with low stock
  const albumPayload = JSON.stringify({
    title: "Failure Test Album",
    artist: "FailBot",
    price: 19.99,
    releaseYear: 2024,
    genre: "EdgeCase",
    initialQuantity: 2,
  });

  const albumRes = http.post(ALBUM_SERVICE_URL, albumPayload, {
    headers: {
      "Content-Type": "application/json",
      "Client-Type": "admin",
    },
  });

  check(albumRes, {
    "Album created for failure test": (r) => r.status === 201,
  });

  const albumId = JSON.parse(albumRes.body).id;
  console.log(`🎵 Created albumId for failure: ${albumId}`);
  sleep(1);

  // Step 2: Place a too-large order to trigger failure
  const orderPayload = JSON.stringify({
    albumId: albumId.toString(),
    quantity: 99, // Exceeds inventory on purpose
    userId: USER_ID,
  });

  const res = http.post(ORDER_SERVICE_URL, orderPayload, {
    headers: {
      "Content-Type": "application/json",
      "Client-Type": "user",
    },
  });

  check(res, {
    "Expected to fail with 4xx due to insufficient inventory": (r) =>
      r.status >= 400 && r.status < 500,
  });

  sleep(1);
}
