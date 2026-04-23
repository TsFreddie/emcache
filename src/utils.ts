import { config } from "./config";

const blockedSet = new Set<string>();

const BLOCKED_REQUEST_HEADERS = ["host", "connection"];

const BLOCKED_RESPONSE_HEADERS = [
  "transfer-encoding",
  "connection",
  "keep-alive",
  "content-encoding",
];

function cleanUpHeaders(
  headers: Headers,
  blockedHeaders: string[],
  overrideHeaders?: Record<string, string>,
) {
  const result = new Headers();

  // Reset and populate blocked set
  blockedSet.clear();
  for (const header of blockedHeaders) {
    blockedSet.add(header.toLowerCase());
  }
  if (overrideHeaders) {
    for (const key of Object.keys(overrideHeaders)) {
      blockedSet.add(key.toLowerCase());
    }
  }

  for (const [key, value] of headers) {
    if (!blockedSet.has(key.toLowerCase())) {
      result.set(key, value);
    }
  }

  if (overrideHeaders) {
    for (const [key, value] of Object.entries(overrideHeaders)) {
      result.set(key, value);
    }
  }

  return result;
}

export function cleanUpRequestHeaders(
  headers: Headers,
  overrideHeaders?: Record<string, string>,
): Headers {
  return cleanUpHeaders(headers, BLOCKED_REQUEST_HEADERS, overrideHeaders);
}

export function cleanUpResponseHeaders(
  headers: Headers,
  overrideHeaders?: Record<string, string>,
): Headers {
  return cleanUpHeaders(headers, BLOCKED_RESPONSE_HEADERS, overrideHeaders);
}

export function proxyRequest(
  request: Request,
  overrideInit?: RequestInit,
  overrideHeaders?: Record<string, string>,
) {
  const url = new URL(request.url);
  const headers = cleanUpRequestHeaders(request.headers, overrideHeaders);

  const upstreamUrl = `${config.upstream.url}${url.pathname + url.search}`;
  const init = {
    method: request.method,
    headers,
    body: request.body,
    signal: request.signal,
  };

  if (overrideInit) {
    Object.assign(init, overrideInit);
  }

  return fetch(upstreamUrl, init);
}
