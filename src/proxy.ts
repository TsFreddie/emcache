import { config } from "./config";
import type { ProxyContext } from "./types";
import { interceptors } from "./interceptor";
import { cleanUpResponseHeaders, proxyRequest } from "./utils";

/**
 * Build the upstream URL from the request
 */
export function buildUpstreamUrl(request: Request): string {
  const url = new URL(request.url);
  const upstreamPath = url.pathname + url.search;
  return `${config.upstream.url}${upstreamPath}`;
}

/**
 * Build proxy context for interceptors
 */
function buildContext(request: Request, upstreamUrl: string): ProxyContext {
  return { request, upstreamUrl };
}

/**
 * Proxy an HTTP request to upstream
 */
export async function proxyHttp(request: Request): Promise<Response> {
  const upstreamUrl = buildUpstreamUrl(request);
  const ctx = buildContext(request, upstreamUrl);

  // Run request interceptors - stop if one returns a Response
  for (const interceptor of interceptors) {
    if (interceptor.onRequest) {
      const result = await interceptor.onRequest(ctx);
      if (result instanceof Response) {
        return result;
      }
    }
  }

  try {
    const response = await proxyRequest(request, { redirect: "manual" });

    // Run response interceptors
    let finalResponse = response;
    for (const interceptor of interceptors) {
      if (interceptor.onResponse) {
        finalResponse = await interceptor.onResponse(ctx, finalResponse);
      }
    }

    return new Response(finalResponse.body, {
      status: finalResponse.status,
      headers: cleanUpResponseHeaders(finalResponse.headers),
    });
  } catch (error) {
    console.error(`[Proxy] Upstream error: ${error}`);
    return new Response(JSON.stringify({ error: "Upstream unavailable" }), {
      status: 502,
      headers: { "Content-Type": "application/json" },
    });
  }
}
