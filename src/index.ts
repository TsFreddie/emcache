import { config } from "./config";
import { proxyHttp } from "./proxy";

// ============================================
// WebSocket data attached to each connection
// ============================================

interface WSData {
  id: number;
  upstream: WebSocket;
}

// ============================================
// Bun.serve HTTP + WebSocket server
// ============================================

const server = Bun.serve<WSData>({
  port: config.server.port,
  hostname: config.server.host,
  idleTimeout: 60,

  // Handle HTTP requests
  async fetch(req) {
    const url = new URL(req.url);
    const path = url.pathname;

    console.log(
      `[${new Date().toISOString()}] ${req.method} ${url.pathname}${url.search}`,
    );

    // Health check
    if (path === "/health") {
      return Response.json({ status: "ok" });
    }

    // WebSocket upgrade requests
    if (req.headers.get("upgrade") === "websocket") {
      return handleWebSocketUpgrade(req);
    }

    // Regular HTTP proxy
    return proxyHttp(req);
  },

  // WebSocket handler
  websocket: {
    open(ws) {
      console.log(`[WS] Client connected: ${ws.data.id}`);

      // Set up upstream message forwarding
      const upstream = ws.data.upstream;
      upstream.addEventListener("message", (event) => {
        if (ws.readyState === WebSocket.OPEN) {
          ws.send(event.data);
        }
      });

      upstream.addEventListener("close", () => {
        if (ws.readyState === WebSocket.OPEN) {
          ws.close(1001, "Upstream closed");
        }
      });

      upstream.addEventListener("error", (error) => {
        console.error(`[WS] Upstream error:`, error);
      });

      console.log(`[WS] Upstream connected for client: ${ws.data.id}`);
    },

    // Called when a message is received from the client
    message(ws, message) {
      const upstream = ws.data.upstream;
      if (upstream.readyState === WebSocket.OPEN) {
        upstream.send(message);
      }
    },

    // Called when the client disconnects
    close(ws, code, reason) {
      console.log(`[WS] Client disconnected: ${ws.data.id} (${code})`);
      const upstream = ws.data.upstream;
      if (upstream.readyState === WebSocket.OPEN) {
        upstream.close(code, reason);
      }
    },
  },
});

// ============================================
// WebSocket handling
// ============================================

// Headers to forward to upstream WebSocket
const FORWARD_HEADERS = [
  "x-emby-authorization",
  "x-emby-client",
  "x-emby-client-version",
  "x-emby-device",
  "x-emby-device-id",
  "x-emby-device-name",
  "x-emby-application-version",
];

function buildUpstreamWsUrl(request: Request): {
  url: string;
  headers: Record<string, string>;
} {
  const url = new URL(request.url);
  const upstreamPath = url.pathname + url.search;
  const wsUrl = `${config.upstream.url}${upstreamPath}`
    .replace("https://", "wss://")
    .replace("http://", "ws://");

  // Build headers to forward
  const headers: Record<string, string> = {};
  for (const name of FORWARD_HEADERS) {
    const value = request.headers.get(name);
    if (value) {
      headers[name] = value;
    }
  }

  // Forward API key if present
  const apiKey =
    request.headers.get("X-Emby-Token") ||
    url.searchParams.get("api_key") ||
    "";

  if (apiKey) {
    return {
      url: `${wsUrl}${wsUrl.includes("?") ? "&" : "?"}api_key=${apiKey}`,
      headers,
    };
  }
  return { url: wsUrl, headers };
}

let id = 0;

async function handleWebSocketUpgrade(
  request: Request,
): Promise<Response | undefined> {
  const { url: wsUrl, headers } = buildUpstreamWsUrl(request);
  console.log(`[WS] Upgrading to: ${wsUrl}`);

  // Create upstream WebSocket with forwarded headers
  const upstream = new WebSocket(wsUrl, { headers });

  // Wait for upstream to connect
  try {
    await new Promise<void>((resolve, reject) => {
      const timeout = setTimeout(
        () => reject(new Error("Connection timeout")),
        10000,
      );

      upstream.addEventListener("open", () => {
        clearTimeout(timeout);
        resolve();
      });

      upstream.addEventListener("error", () => {
        clearTimeout(timeout);
        reject(new Error("Upstream error"));
      });
    });
  } catch (error) {
    console.error(`[WS] Upstream connection failed:`, error);
    upstream.close();
    return new Response("Upstream unavailable", { status: 502 });
  }

  console.log(`[WS] Upstream connected, upgrading client...`);

  // Upgrade client connection with upstream in data
  const upgraded = server.upgrade(request, {
    data: { upstream, id: id++ } as WSData,
  });

  if (!upgraded) {
    console.error(`[WS] Failed to upgrade connection`);
    upstream.close();
    return;
  }

  // Return undefined - Bun handles the 101 response automatically
  return;
}

// ============================================
// Start
// ============================================

console.log(
  `🚀 Emby Proxy running on http://${config.server.host}:${config.server.port}`,
);
console.log(`   Upstream: ${config.upstream.url}`);
console.log(
  `   WebSocket: ws://${config.server.host}:${config.server.port}/embywebsocket`,
);

// Cleanup on exit
process.on("SIGINT", async () => {
  console.log("\nShutting down...");
  await server.stop(true);
  process.exit(0);
});
