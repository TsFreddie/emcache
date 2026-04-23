/**
 * Fetch with bandwidth throttling.
 * Limits the download speed to maxBytesPerSecond.
 * Returns immediately with a streaming Response.
 */

const maxBytesPerSecond = 512 * 1024;

export async function slowFetch(
  url: string | URL,
  init?: RequestInit,
): Promise<Response> {
  const response = await fetch(url, init);

  if (!response.body) {
    return response;
  }

  const bytesPerMs = maxBytesPerSecond / 1000;
  const msPerByte = 1 / bytesPerMs;

  let reader: ReadableStreamDefaultReader<Uint8Array> | null = null;
  let resumeTime = 0;

  const throttledBody = new ReadableStream({
    async pull(controller) {
      if (!reader) {
        reader =
          response.body!.getReader() as ReadableStreamDefaultReader<Uint8Array>;
      }

      const now = Date.now();

      // Wait for throttle delay
      if (resumeTime > now) {
        const waitMs = resumeTime - now;
        if (waitMs > 0) {
          await new Promise((resolve) => setTimeout(resolve, waitMs));
        }
      }

      // Read next chunk
      const { done, value } = await reader.read();

      if (done) {
        controller.close();
        return;
      }

      // Enqueue the chunk
      controller.enqueue(value);

      // Schedule next chunk's delay
      const chunkDelay = value.byteLength * msPerByte;
      resumeTime = Date.now() + chunkDelay;
    },

    cancel() {
      // Clean up reader when stream is cancelled
      if (reader) {
        reader.cancel();
        reader = null;
      }
    },
  });

  return new Response(throttledBody, {
    status: response.status,
    statusText: response.statusText,
    headers: response.headers,
  });
}
