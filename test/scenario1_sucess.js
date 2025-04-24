import http from "k6/http";
import { check, sleep } from "k6";

const BASE_URL = "http://localhost";
const ALBUM_SERVICE_URL = `${BASE_URL}:8080/api/albums`;
const ORDER_SERVICE_URL = `${BASE_URL}:8082/api/orders`;
const USER_ID = "u1";

export const options = {
  vus: 1,
  iterations: 1,
};

export default function () {
  // Step 1: Create album (ensure albumId=1 exists)
  const albumPayload = JSON.stringify({
    title: "Scenario1 Test",
    artist: "Tester",
    price: 9.99,
    releaseYear: 2024,
    genre: "Scenario",
    initialQuantity: 5,
  });

  const albumRes = http.post(ALBUM_SERVICE_URL, albumPayload, {
    headers: {
      "Content-Type": "application/json",
      "Client-Type": "admin",
    },
  });

  check(albumRes, {
    "Album created": (r) => r.status === 201,
  });

  const albumId = JSON.parse(albumRes.body).id;
  console.log(`🎵 Created albumId: ${albumId}`);
  sleep(1);

  // Step 2: Place order
  const orderPayload = JSON.stringify({
    albumId: albumId.toString(),
    quantity: 1,
    userId: USER_ID,
  });

  const res = http.post(ORDER_SERVICE_URL, orderPayload, {
    headers: {
      "Content-Type": "application/json",
      "Client-Type": "user",
    },
  });

  check(res, {
    "Order created successfully (201)": (r) => r.status === 201,
  });

  sleep(1);
}
