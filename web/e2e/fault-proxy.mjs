// Drops the RESPONSE of selected Task submits so the product must recover the
// original request by its idempotency key. Everything else is relayed untouched.
// FAULT_UPSTREAM=<Task base URL> FAULT_PORT=32323 FAULT_DROP_SUBMITS=2 FAULT_LOG=<file>
import http from "node:http";
import https from "node:https";
import fs from "node:fs";

const upstream = new URL(process.env.FAULT_UPSTREAM ?? "");
const PORT = Number(process.env.FAULT_PORT ?? 32323);
const drop = new Set(String(process.env.FAULT_DROP_SUBMITS ?? "").split(",").filter(Boolean).map(Number));
const log = { submits_seen: 0, dropped: [], dropped_submits: [] };
const save = () => process.env.FAULT_LOG && fs.writeFileSync(process.env.FAULT_LOG, JSON.stringify(log));
save();

http.createServer((req, res) => {
  const submit = req.method === "POST" && new URL(req.url, "http://x").pathname === "/internal/v1/tasks";
  const n = submit ? ++log.submits_seen : 0;
  const lib = upstream.protocol === "https:" ? https : http;
  const body = [];
  req.on("data", (c) => body.push(c));
  req.on("end", () => {
    const payload = Buffer.concat(body);
    // Attribution: which product request this submit carried, so the verdict can tie a drop to one run.
    let who = {};
    if (submit) { try { const j = JSON.parse(payload.toString()); who = { usage_id: j.billing?.usage_id ?? null, idempotency_key: j.idempotency_key ?? null }; } catch { who = { usage_id: null, idempotency_key: null }; } }
    const out = lib.request({ protocol: upstream.protocol, hostname: upstream.hostname, port: upstream.port, method: req.method, path: upstream.pathname.replace(/\/$/, "") + req.url, headers: { ...req.headers, host: upstream.host } }, (up) => {
      const chunks = [];
      up.on("data", (c) => chunks.push(c));
      up.on("end", () => {
        if (submit && drop.has(n)) { log.dropped.push(n); log.dropped_submits.push({ n, ...who }); save(); req.socket.destroy(); return; } // the platform already acted; the caller never hears back
        res.writeHead(up.statusCode ?? 502, up.headers);
        res.end(Buffer.concat(chunks));
        save();
      });
    });
    out.on("error", () => { res.writeHead(502); res.end(); });
    out.end(payload);
  });
}).listen(PORT, "127.0.0.1", () => console.log(`fault proxy on ${PORT} -> ${upstream.origin}, dropping submit responses ${[...drop].join(",") || "none"}`));
