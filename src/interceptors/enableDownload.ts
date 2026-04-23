import type { Interceptor } from "../types";

export const enableDownload: Interceptor = {
  onRequest(ctx) {
    const url = new URL(ctx.request.url);
    const path = url.pathname;

    // Check if this is an Emby video stream/download route
    if (!path.startsWith("/emby/Items/") || !path.endsWith("/Download")) {
      return;
    }
    console.log("[Download] Url", ctx.request.url);
    console.log("[Download] Intercepted:", ctx.request.method, path);
    console.log("[Download] Headers:");
    ctx.request.headers.forEach((value, key) => {
      console.log(`  ${key}: ${value}`);
    });

    return new Response(undefined, {
      status: 403,
      statusText: "Forbidden",
    });
  },
  async onResponse(ctx, response) {
    const path = new URL(ctx.request.url).pathname;

    // Only process JSON responses from relevant endpoints
    if (
      !path.startsWith("/emby/Items") &&
      !path.startsWith("/emby/Videos") &&
      !path.startsWith("/emby/Users")
    ) {
      return response;
    }

    const contentType = response.headers.get("Content-Type") || "";
    if (!contentType.includes("application/json")) {
      return response;
    }

    try {
      const text = await response.text();
      const json = JSON.parse(text);

      // Recursively set CanDownload = true
      const setCanDownload = (obj: unknown): void => {
        if (obj && typeof obj === "object") {
          const o = obj as Record<string, unknown>;
          if ("CanDownload" in o) o.CanDownload = true;
          // Also enable via policy (used by Emby clients)
          if (o.Policy && typeof o.Policy === "object") {
            (o.Policy as Record<string, unknown>).EnableContentDownloading =
              true;
          }
          // Recurse into arrays
          for (const key of Object.keys(o)) {
            if (Array.isArray(o[key])) {
              (o[key] as unknown[]).forEach(setCanDownload);
            }
          }
        }
      };

      setCanDownload(json);

      const newHeaders = new Headers(response.headers);

      return new Response(JSON.stringify(json), {
        status: response.status,
        headers: newHeaders,
      });
    } catch {
      // Not valid JSON, return as-is
      return response;
    }
  },
};
