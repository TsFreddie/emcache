import { getMediaSource } from "../item";
import { PrimarySession } from "../session";
import type { Interceptor } from "../types";
import { proxyRequest } from "../utils";

/**
 * Stream saver interceptor - intercepts Emby video routes
 */
export const streamSaver: Interceptor = {
  async onRequest(ctx) {
    const url = new URL(ctx.request.url.toLowerCase());
    const path = url.pathname;

    // check if this is an Emby video stream route
    const match = path.match(
      /^\/emby\/videos\/([0-9]+)\/(stream|original)\.([a-z0-9]+)$/,
    );
    if (!match) {
      return;
    }

    // can't proxy m3u8 or hls files
    if (path.endsWith(".m3u8") || path.endsWith(".hls")) {
      console.log("[StreamSaver] Skipped:", path);
      return;
    }

    // only support range requests
    if (!ctx.request.headers.get("range")) {
      // still proxy the request, just in case we only want to use this for proxying the video stream
      return proxyRequest(ctx.request);
    }

    const isStatic =
      match[2]! === "original" || url.searchParams.get("static") === "true";

    // don't cache non original files
    if (!isStatic) {
      return proxyRequest(ctx.request);
    }

    // get mediaSourceId
    const mediaSourceId = url.searchParams.get("mediasourceid");
    if (!mediaSourceId) {
      return proxyRequest(ctx.request);
    }

    const mediaSource = getMediaSource(mediaSourceId);
    if (!mediaSource) {
      // couldn't find cached metadata
      return proxyRequest(ctx.request);
    }

    // get session id
    return proxyRequest(ctx.request);
  },
};
