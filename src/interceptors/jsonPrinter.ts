import type { Interceptor } from "../types";
import { proxyRequest } from "../utils";

export const jsonPrinter: Interceptor = {
  // async onRequest(ctx) {
  //   if (new URL(ctx.request.url).pathname.endsWith("PlaybackInfo")) {
  //     console.log("[JSON] PlaybackInfo:", ctx.request.method, ctx.request.url);
  //     console.log("[JSON] Params:", new URL(ctx.request.url).search);
  //     const body = await ctx.request.text();
  //     console.log("[JSON] Body:", body);

  //     return proxyRequest(ctx.request, { body: body });
  //   }
  // },

  async onResponse(ctx, response) {
    if (response.headers.get("content-type")?.startsWith("application/json")) {
      if (new URL(ctx.request.url).pathname.endsWith("PlaybackInfo")) {
        console.log(
          "[JSON] PlaybackInfo:",
          response.status,
          response.statusText,
        );
        console.log("[JSON] Response:", response.status, response.statusText);
        const body = await response.text();

        console.log("[JSON] Params:", new URL(ctx.request.url).search);
        console.log("[JSON] Body:", body);

        return new Response(body, {
          status: response.status,
          statusText: response.statusText,
          headers: response.headers,
        });
      } else {
        return response;
      }

      console.log("[JSON] Response:", response.status, response.statusText);
      const body = await response.text();

      console.log("[JSON] Params:", new URL(ctx.request.url).search);
      console.log("[JSON] Body:", body);

      return new Response(body, {
        status: response.status,
        statusText: response.statusText,
        headers: response.headers,
      });
    }
    return response;
  },
};
