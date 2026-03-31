import ws from "k6/ws";
import http from "k6/http";
import { check, sleep } from "k6";
import { Counter } from "k6/metrics";
import encoding from "k6/encoding";

// ─── Config ────────────────────────────────────────────────────────────────
const BASE_URL = "http://localhost:8080";
const WS_URL = "ws://localhost:8080";
const TOKEN =
  "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VyX2lkIjoiODM0ZDQ2YmUtZTExNS00MzUyLThjM2EtNWFhM2NkNDVlZTg4IiwiZW1haWwiOiJhQGV4YW1wbGUuY29tIiwiZXhwIjoxNzc1MDI5NjQzLCJpYXQiOjE3NzQ5NDMyNDN9.5Zo2thoCWz8FNQnskTpR8tjJ5Co0upjEmGbD3MKmxWc";
const ROOM_ID = "6aa80a9a-3b37-4a1a-b0ae-1fceef69119d";
const FILE_ID = "68175edb-3251-4580-9fe0-2e3e01893c62";
const COOKIE = `parily_token=${TOKEN}`;

// ─── Custom metrics ─────────────────────────────────────────────────────────
const wsConnected = new Counter("ws_connected");
const wsFailed = new Counter("ws_failed");
const saveAccepted = new Counter("save_accepted");
const saveFailed = new Counter("save_failed");
const execAccepted = new Counter("exec_accepted");
const execRejected = new Counter("exec_rejected_already_running");

// ─── Scenarios ──────────────────────────────────────────────────────────────
// Three independent scenarios running in parallel.
// Each targets a different thing we want to prove.
export const options = {
  scenarios: {

    // Scenario 1 — 25 virtual users all connect to the room WebSocket
    // simultaneously and hold the connection for 10s then disconnect cleanly.
    // Watch active_websocket_connections in Grafana — should spike to 25
    // and return to 0 after all users disconnect.
    websocket_connections: {
      executor: "ramping-vus",
      startVUs: 0,
      stages: [
        { duration: "5s", target: 25 },  // ramp up to 25 connections
        { duration: "10s", target: 25 }, // hold — watch Grafana spike
        { duration: "5s", target: 0 },   // ramp down — watch Grafana return to 0
      ],
      exec: "scenarioWebSocket",
    },

    // Scenario 2 — 10 virtual users all POST the exact same Yjs blob to
    // SaveState simultaneously. Dedup should skip all duplicates after the
    // first one lands. Watch yjs_saves_skipped_total spike in Grafana.
    simultaneous_saves: {
      executor: "constant-vus",
      vus: 10,
      duration: "20s",
      exec: "scenarioSaveState",
      startTime: "5s", // slight delay so WS scenario starts first
    },

    // Scenario 3 — 15 virtual users all click Run at the same time.
    // Redis SetNX lock means only ONE execution goes through.
    // The rest get already_running back immediately.
    // Watch executions_total stay at 1 per wave in Grafana.
    concurrent_executions: {
      executor: "ramping-vus",
      startVUs: 0,
      stages: [
        { duration: "2s", target: 15 },  // ramp up fast — simulate everyone clicking Run
        { duration: "8s", target: 15 },  // hold
        { duration: "2s", target: 0 },
      ],
      exec: "scenarioExecution",
      startTime: "10s", // start after saves scenario is warmed up
    },
  },

  // Global thresholds — test fails if these are breached
  thresholds: {
    // At least 90% of WebSocket connections must succeed
    ws_connected: [{ threshold: "count>20", abortOnFail: false }],
    // SaveState must accept at least some saves (not all rejected)
    save_accepted: [{ threshold: "count>5", abortOnFail: false }],
    // HTTP errors should be under 10%
    http_req_failed: [{ threshold: "rate<0.1", abortOnFail: false }],
  },
};

// ─── Scenario 1 — WebSocket connections ─────────────────────────────────────
export function scenarioWebSocket() {
  const url = `${WS_URL}/room-ws/${ROOM_ID}`;

  const res = ws.connect(url, { headers: { Cookie: COOKIE } }, (socket) => {
    socket.on("open", () => {
      wsConnected.add(1);

      // send heartbeat every 10s as the real client does
      socket.setInterval(() => {
        socket.send(JSON.stringify({ type: "heartbeat" }));
      }, 10000);
    });

    socket.on("error", () => {
      wsFailed.add(1);
    });

    // hold connection for the duration of the stage
    socket.setTimeout(() => {
      socket.close();
    }, 12000);
  });

  check(res, { "ws connected": (r) => r && r.status === 101 });
}

// ─── Scenario 2 — Simultaneous SaveState (dedup under load) ─────────────────
// A minimal valid Yjs document encoded as base64.
// All 10 VUs send the exact same blob — dedup should skip all but the first.
const YJS_BLOB = generateMinimalYjsBlob();

export function scenarioSaveState() {
  const url = `${BASE_URL}/api/rooms/${ROOM_ID}/files/${FILE_ID}/state`;

  const res = http.post(url, YJS_BLOB, {
    headers: {
      Cookie: COOKIE,
      "Content-Type": "application/octet-stream",
    },
  });

  if (res.status === 200) {
    saveAccepted.add(1);
  } else {
    saveFailed.add(1);
  }

  check(res, { "save returned 200": (r) => r.status === 200 });

  // small sleep so we don't hammer too fast — realistic save interval
  sleep(2);
}

// ─── Scenario 3 — Concurrent executions (lock under load) ───────────────────
export function scenarioExecution() {
  // connect to room WebSocket and send run_file — same as real frontend does
  const url = `${WS_URL}/room-ws/${ROOM_ID}`;

  ws.connect(url, { headers: { Cookie: COOKIE } }, (socket) => {
    socket.on("open", () => {
      // send run_file immediately on connect — simulates everyone clicking Run
      socket.send(
        JSON.stringify({
          type: "run_file",
          file_id: FILE_ID,
          execution_id: generateUUID(),
        })
      );
    });

    socket.on("message", (data) => {
      try {
        const msg = JSON.parse(data);
        if (msg.type === "executing") {
          execAccepted.add(1);
        } else if (
          msg.type === "execution_error" &&
          msg.reason === "already_running"
        ) {
          // this is EXPECTED — proves the lock is working
          execRejected.add(1);
        }
      } catch (_) {}
    });

    socket.setTimeout(() => {
      socket.close();
    }, 5000);
  });

  sleep(3);
}

// ─── Helpers ────────────────────────────────────────────────────────────────

// Real Yjs document state fetched directly from MongoDB via LoadState endpoint.
// This is the actual binary blob for main.py in the test room.
// All 10 VUs send this exact same blob — dedup should skip all but the first
// because yjsBlobToText() will decode to the same text on every request.
// k6 sends Uint8Array directly as binary HTTP body — no .buffer wrapper needed.
function generateMinimalYjsBlob() {
  const base64 =
    "AgH+r6/dDQCE/c6/nQnrAfwCaW1wb3J0IHN5cwppbXBvcnQgdGltZQoKZGVmIGRpdmlkZShhLCBiKToKICAgIGlmIGIgPT0gMDoKICAgICAgICBwcmludCgiRXJyb3I6IGRpdmlzaW9uIGJ5IHplcm8iLCBmaWxlPXN5cy5zdGRlcnIpCiAgICAgICAgc3lzLmV4aXQoMSkKICAgIHJldHVybiBhIC8gYgoKcHJpbnQoIlN0YXJ0aW5nIGNhbGN1bGF0aW9ucy4uLiIpCnRpbWUuc2xlZXAoMSkKcHJpbnQoZiIxMCAvIDIgPSB7ZGl2aWRlKDEwLCAyKX0iKQpwcmludChmIjkgLyAzID0ge2RpdmlkZSg5LCAzKX0iKQpwcmludCgiQWJvdXQgdG8gY3Jhc2guLi4iKQp0aW1lLnNsZWVwKDEpCnByaW50KGYiNSAvIDAgPSB7ZGl2aWRlKDUsIDApfSIpCnByaW50KCJUaGlzIGxpbmUgc2hvdWxkIG5ldmVyIHByaW50IikB/c6/nQkAAQEHY29udGVudOwBAf3Ov50JAQDsAQ==";
  return encoding.b64decode(base64, "std", "u");
}

// Generates a UUID v4 — used for execution_id in scenario 3
// Each VU sends a unique execution_id so the idempotency cache
// doesn't block them — only the Redis lock should block them
function generateUUID() {
  return "xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx".replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0;
    const v = c === "x" ? r : (r & 0x3) | 0x8;
    return v.toString(16);
  });
}