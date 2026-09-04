// Custom HTTPS server for production: terminates TLS directly (reusing the mail
// server's own Let's Encrypt certificate) the same way patrabahokd's admin dashboard
// does, rather than requiring a separate reverse proxy. Also hot-reloads the
// certificate on renewal, mirroring cli/internal/webui/server.go's certReloader.
const { createServer } = require("https");
const { readFileSync, statSync } = require("fs");
const next = require("next");

const dev = process.env.NODE_ENV !== "production";
const port = Number(process.env.PORT || 3443);
const certPath = process.env.TLS_CERT;
const keyPath = process.env.TLS_KEY;

if (!certPath || !keyPath) {
  console.error("TLS_CERT and TLS_KEY environment variables are required.");
  process.exit(1);
}

const app = next({ dev });
const handle = app.getRequestHandler();

let lastMtimeMs = 0;

function loadSecureContext(server) {
  const mtimeMs = statSync(certPath).mtimeMs;
  if (mtimeMs === lastMtimeMs) return;
  const cert = readFileSync(certPath);
  const key = readFileSync(keyPath);
  server.setSecureContext({ cert, key });
  lastMtimeMs = mtimeMs;
  console.log(`patrabahok-mail: TLS certificate (re)loaded from ${certPath}`);
}

app.prepare().then(() => {
  const cert = readFileSync(certPath);
  const key = readFileSync(keyPath);
  lastMtimeMs = statSync(certPath).mtimeMs;

  const server = createServer({ cert, key }, (req, res) => handle(req, res));

  // Certbot renews roughly every ~60 days; checking hourly is far more than enough
  // headroom without adding meaningful overhead.
  setInterval(() => {
    try {
      loadSecureContext(server);
    } catch (err) {
      console.error("patrabahok-mail: failed to reload TLS certificate:", err);
    }
  }, 60 * 60 * 1000).unref();

  server.listen(port, () => {
    console.log(`patrabahok-mail listening on https://0.0.0.0:${port}`);
  });
});
