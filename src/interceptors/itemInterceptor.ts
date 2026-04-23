import { processMediaSource } from "../item";
import type { Interceptor } from "../types";

export const itemInterceptor: Interceptor = {
  async onResponse(ctx, response) {
    const url = new URL(ctx.request.url);
    const pathname = url.pathname;
    if (pathname.match(/^\/emby\/Users\/[0-9a-f]+\/Items\/([0-9]+)\/?/)) {
      const body = await response.text();

      try {
        const data = JSON.parse(body);
        console.log(`[itemInterceptor] Processing item ${data.Id}, MediaSources: ${data.MediaSources?.length ?? 0}`);
        processMediaSource(data);
      } catch (e) {
        console.error(e);
      }

      return new Response(body, {
        status: response.status,
        statusText: response.statusText,
        headers: response.headers,
      });
    }

    return response;
  },
};
