import ws from "k6/ws";
import http from "k6/http";
import { check, sleep } from "k6";
import { Counter } from "k6/metrics";
import encoding from "k6/encoding";

// ─── Config ────────────────────────────────────────────────────────────────
const BASE_URL = "http://localhost:8080";
const WS_URL = "ws://localhost:8080";
const TOKEN =
  "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VyX2lkIjoiODk3M2Q1ZmEtYzEzNC00ZDAwLWE3YmMtOWFmMjhiNjE5Njg4IiwiZW1haWwiOiJzYW15QGV4YW1wbGUuY29tIiwiZXhwIjoxNzc1MjAzMDk2LCJpYXQiOjE3NzUxMTY2OTZ9.1E5er-qxHEnv61B14rmTxNCeKPC591emJ7erpmqgEys";
const ROOM_ID = "0d9c52f5-f4c5-444d-9fe8-b588e2f91121";
const FILE_ID = "4b028aab-eab7-4920-97c1-a4e5891a3cf4";
const COOKIE = `parily_token=${TOKEN}`;

// ─── Custom metrics ─────────────────────────────────────────────────────────
const wsConnected = new Counter("ws_connected");
const wsFailed = new Counter("ws_failed");
const saveAccepted = new Counter("save_accepted");
const saveFailed = new Counter("save_failed");
const execAccepted = new Counter("exec_accepted");
const execRejected = new Counter("exec_rejected_already_running");

// ─── Scenarios ──────────────────────────────────────────────────────────────
export const options = {
  scenarios: {

    // Scenario 1 — 25 virtual users connect to the room WebSocket and hold
    // for 60 seconds. Grafana scrapes every 15s so the plateau will be
    // clearly visible as a flat line at 25 connections for ~4 scrape intervals.
    websocket_connections: {
      executor: "ramping-vus",
      startVUs: 0,
      stages: [
        { duration: "10s", target: 25 }, // ramp up
        { duration: "60s", target: 25 }, // hold — Grafana shows clear plateau
        { duration: "10s", target: 0 },  // ramp down
      ],
      exec: "scenarioWebSocket",
    },

    // Scenario 2 — 10 virtual users all POST the exact same Yjs blob for 60s.
    // Dedup skips all duplicates. Watch yjs_saves_skipped_total sustain in Grafana.
    simultaneous_saves: {
      executor: "constant-vus",
      vus: 10,
      duration: "60s",
      exec: "scenarioSaveState",
      startTime: "10s",
    },

    // Scenario 3 — 15 virtual users all click Run for 50 seconds.
    // Redis SetNX lock means only ONE execution goes through per wave.
    // Watch executions_total stay low while exec_rejected climbs in Grafana.
    concurrent_executions: {
      executor: "ramping-vus",
      startVUs: 0,
      stages: [
        { duration: "5s", target: 15 },  // ramp up fast
        { duration: "50s", target: 15 }, // hold — sustained execution spike
        { duration: "5s", target: 0 },
      ],
      exec: "scenarioExecution",
      startTime: "15s",
    },
  },

  thresholds: {
    ws_connected: [{ threshold: "count>20", abortOnFail: false }],
    save_accepted: [{ threshold: "count>5", abortOnFail: false }],
    http_req_failed: [{ threshold: "rate<0.1", abortOnFail: false }],
  },
};

// ─── Scenario 1 — WebSocket connections ─────────────────────────────────────
export function scenarioWebSocket() {
  const url = `${WS_URL}/room-ws/${ROOM_ID}`;

  const res = ws.connect(url, { headers: { Cookie: COOKIE } }, (socket) => {
    socket.on("open", () => {
      wsConnected.add(1);

      // heartbeat every 10s — same as real frontend
      socket.setInterval(() => {
        socket.send(JSON.stringify({ type: "heartbeat" }));
      }, 10000);
    });

    socket.on("error", () => {
      wsFailed.add(1);
    });

    // hold for 65s so the plateau is fully captured by Grafana
    socket.setTimeout(() => {
      socket.close();
    }, 65000);
  });

  check(res, { "ws connected": (r) => r && r.status === 101 });
}

// ─── Scenario 2 — Simultaneous SaveState (dedup under load) ─────────────────
// Real Yjs blob for main.py fetched via LoadState endpoint.
// All 10 VUs send this exact blob — dedup skips all but the first.
const YJS_BLOB = generateYjsBlob();

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
  sleep(2);
}

// ─── Scenario 3 — Concurrent executions (lock under load) ───────────────────
export function scenarioExecution() {
  const url = `${WS_URL}/room-ws/${ROOM_ID}`;

  ws.connect(url, { headers: { Cookie: COOKIE } }, (socket) => {
    socket.on("open", () => {
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
          // expected — proves the Redis lock is working
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

// Real Yjs state for main.py fetched via LoadState endpoint.
function generateYjsBlob() {
  const base64 =
    "AhKlwamoCACBn9/73QWBAgGBpcGpqAgAAcGlwamoCAClwamoCAESwaXBqagIE6XBqagIAQHBpcGpqAgTpcGpqAgUAsGlwamoCBSlwamoCAENwaXBqagII6XBqagIAQHBpcGpqAgjpcGpqAgkCMGlwamoCCylwamoCCQBwaXBqagILKXBqagILQLBpcGpqAgvpcGpqAgtAcGlwamoCC+lwamoCDABwaXBqagILaXBqagIJAXBpcGpqAg2pcGpqAgkAcGlwamoCDalwamoCDcGwaXBqagIPaXBqagINwHBpcGpqAg9pcGpqAg+AcGlwamoCDelwamoCCQBMp/f+90FAAEBB2NvbnRlbnQLgZ/f+90FCgHBn9/73QUKn9/73QULDIGf3/vdBQsxgZ/f+90FSAHBn9/73QVIn9/73QVJAYGf3/vdBUk0gZ/f+90FfgHBn9/73QV+n9/73QV/AYGf3/vdBX8HgZ/f+90FhwEBwZ/f+90FhwGf3/vdBYgBA4Gf3/vdBYgBCIGf3/vdBZMBAcGf3/vdBZMBn9/73QWUAQOBn9/73QWUARKBn9/73QWpAQHBn9/73QWpAZ/f+90FqgEBgZ/f+90FqgEPgZ/f+90FugECgZ/f+90FvAEBwZ/f+90FvAGf3/vdBb0BAcGf3/vdBboBn9/73QW7AQGBn9/73QW9AQ2Bn9/73QXMAQHBn9/73QXMAZ/f+90FzQEBgZ/f+90FzQEIgZ/f+90F1gEBwZ/f+90F1gGf3/vdBdcBA4Gf3/vdBdcBCIGf3/vdBeIBAcGf3/vdBeIBn9/73QXjAQOBn9/73QXjARSBn9/73QX6AQHBn9/73QX6AZ/f+90F+wEBgZ/f+90F+wEFwZ/f+90FgQKlwamoCAALwZ/f+90FjAKlwamoCAABwZ/f+90FjAKf3/vdBY0CAcGf3/vdBY0CpcGpqAgACMGf3/vdBZYCpcGpqAgAAcGf3/vdBZYCn9/73QWXAgPBn9/73QWXAqXBqagIAAjBn9/73QWiAqXBqagIAAHBn9/73QWiAp/f+90FowIDwZ/f+90FowKlwamoCAARwZ/f+90FtwKlwamoCAABwZ/f+90FtwKf3/vdBbgCAcGf3/vdBbgCpcGpqAgABYSlwamoCAHADGltcG9ydCB0aW1lCmltcG9ydCByYW5kb20KCmRlZiBidWJibGVfc29ydChhcnIpOgogICAgbiA9IGxlbihhcnIpCiAgICBmb3IgaSBpbiByYW5nZShuKToKICAgICAgICBmb3IgaiBpbiByYW5nZSgwLCBuLWktMSk6CiAgICAgICAgICAgIGlmIGFycltqXSA+IGFycltqKzFdOgogICAgICAgICAgICAgICAgYXJyW2pdLCBhcnJbaisxXSA9IGFycltqKzFdLCBhcnJbal0KICAgIHJldHVybiBhcnIKCmRlZiBtZXJnZV9zb3J0KGFycik6CiAgICBpZiBsZW4oYXJyKSA8PSAxOgogICAgICAgIHJldHVybiBhcnIKICAgIG1pZCA9IGxlbihhcnIpIC8vIDIKICAgIGxlZnQgPSBtZXJnZV9zb3J0KGFycls6bWlkXSkKICAgIHJpZ2h0ID0gbWVyZ2Vfc29ydChhcnJbbWlkOl0pCiAgICByZXR1cm4gbWVyZ2UobGVmdCwgcmlnaHQpCgpkZWYgbWVyZ2UobGVmdCwgcmlnaHQpOgogICAgcmVzdWx0ID0gW10KICAgIGkgPSBqID0gMAogICAgd2hpbGUgaSA8IGxlbihsZWZ0KSBhbmQgaiA8IGxlbihyaWdodCk6CiAgICAgICAgaWYgbGVmdFtpXSA8PSByaWdodFtqXToKICAgICAgICAgICAgcmVzdWx0LmFwcGVuZChsZWZ0W2ldKQogICAgICAgICAgICBpICs9IDEKICAgICAgICBlbHNlOgogICAgICAgICAgICByZXN1bHQuYXBwZW5kKHJpZ2h0W2pdKQogICAgICAgICAgICBqICs9IDEKICAgIHJlc3VsdC5leHRlbmQobGVmdFtpOl0pCiAgICByZXN1bHQuZXh0ZW5kKHJpZ2h0W2o6XSkKICAgIHJldHVybiByZXN1bHQKCmRlZiBiaW5hcnlfc2VhcmNoKGFyciwgdGFyZ2V0KToKICAgIGxlZnQsIHJpZ2h0ID0gMCwgbGVuKGFycikgLSAxCiAgICB3aGlsZSBsZWZ0IDw9IHJpZ2h0OgogICAgICAgIG1pZCA9IChsZWZ0ICsgcmlnaHQpIC8vIDIKICAgICAgICBpZiBhcnJbbWlkXSA9PSB0YXJnZXQ6CiAgICAgICAgICAgIHJldHVybiBtaWQKICAgICAgICBlbGlmIGFyclttaWRdIDwgdGFyZ2V0OgogICAgICAgICAgICBsZWZ0ID0gbWlkICsgMQogICAgICAgIGVsc2U6CiAgICAgICAgICAgIHJpZ2h0ID0gbWlkIC0gMQogICAgcmV0dXJuIC0xCgojIEdlbmVyYXRlIGRhdGEKZGF0YSA9IFtyYW5kb20ucmFuZGludCgxLCAxMDAwKSBmb3IgXyBpbiByYW5nZSgyMCldCnByaW50KGYiT3JpZ2luYWw6ICAgIHtkYXRhfSIpCgojIEJ1YmJsZSBzb3J0CmJ1YmJsZSA9IGRhdGEuY29weSgpCmJ1YmJsZV9zb3J0KGJ1YmJsZSkKcHJpbnQoZiJCdWJibGUgc29ydDoge2J1YmJsZX0iKQoKIyBNZXJnZSBzb3J0Cm1lcmdlZCA9IG1lcmdlX3NvcnQoZGF0YS5jb3B5KCkpCnByaW50KGYiTWVyZ2Ugc29ydDogIHttZXJnZWR9IikKCiMgQmluYXJ5IHNlYXJjaAp0YXJnZXQgPSBtZXJnZWRbcmFuZG9tLnJhbmRpbnQoMCwgbGVuKG1lcmdlZCktMSldCmlkeCA9IGJpbmFyeV9zZWFyY2gobWVyZ2VkLCB0YXJnZXQpCnByaW50KGYiXG5TZWFyY2hpbmcgZm9yIHt0YXJnZXR9IOKGkiBmb3VuZCBhdCBpbmRleCB7aWR4fSIpCgojIFN0YXRzCnByaW50KGYiXG5NaW46IHttZXJnZWRbMF19IikKcHJpbnQoZiJNYXg6IHttZXJnZWRbLTFdfSIpCnByaW50KGYiU3VtOiB7c3VtKG1lcmdlZCl9IikKcHJpbnQoZiJBdmc6IHtzdW0obWVyZ2VkKS9sZW4obWVyZ2VkKTouMmZ9IikCpcGpqAgBAEGf3/vdBQEAvwI=";
  return encoding.b64decode(base64, "std", "u");
}

// Generates a UUID v4 — used for execution_id in scenario 3.
// Each VU sends a unique execution_id so the idempotency cache
// doesn't block them — only the Redis lock should block them.
function generateUUID() {
  return "xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx".replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0;
    const v = c === "x" ? r : (r & 0x3) | 0x8;
    return v.toString(16);
  });
}