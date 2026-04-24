import { Readable } from "node:stream";
import { CHUNK_SIZE, File, FileHandle, FILES } from "./file";
import { config } from "./config";
import { randomUUID } from "node:crypto";
import { cleanUpRequestHeaders } from "./utils";
import playbackBodyData from "./playback.json";
import { getMediaSource, type MediaSource } from "./item";

const STREAMS: Streamer[] = [];

const PLAYBACK_BODY = JSON.stringify(playbackBodyData);

export type SessionType = "primary" | "passthrough" | "passive";

export class Streamer {
  #url: string | URL;
  #headers: Headers;
  #order: number;
  #startByte: number;
  #currentChunk: number;
  #file: File;

  constructor(
    url: string | URL,
    headers: Headers,
    chunk: number,
    file: File,
    order: number,
  ) {
    this.#startByte = chunk * CHUNK_SIZE;
    this.#currentChunk = chunk;
    this.#headers = cleanUpRequestHeaders(headers, {
      range: `bytes=${this.#startByte}-`,
    });
    this.#url = url;
    this.#order = order;
    this.#file = file;

    STREAMS.push(this);
  }

  async start() {
    const response = await fetch(this.#url, {
      headers: this.#headers,
    });

    if (!response.ok) {
      console.error(
        "[Streamer] Response not ok",
        response.status,
        response.statusText,
      );
      return;
    }

    if (!response.body) {
      console.error("[Streamer] No response body");
      return;
    }

    // check if the response range matches
    const responseRange = response.headers.get("Content-Range");
    if (
      responseRange !==
      `bytes ${this.#startByte}-${this.#file.size - 1}/${this.#file.size}`
    ) {
      console.error("[Streamer] Invalid Response Range:", responseRange);
      return;
    }

    // stream the response in chunk
    const readable = Readable.fromWeb(response.body);
    const buffer = Buffer.alloc(CHUNK_SIZE);
    let length = 0;

    let chunkWriter;

    try {
      chunkWriter = this.#file.claimChunk(this.#currentChunk++);
    } catch (e) {
      // chunk already processed
      return;
    }

    try {
      for await (const buf of readable) {
        if (!(buf instanceof Buffer)) return;
        let offset = 0;
        while (length + buf.length - offset >= CHUNK_SIZE) {
          const remaining = CHUNK_SIZE - length;
          buf.copy(buffer, length, offset, remaining);
          length = 0;
          offset += remaining;

          // sync resolve, this makes sure the reader doesn't our run the streamer
          chunkWriter.resolve(Buffer.from(buffer)).catch((err) => {
            console.log("Error writing chunk", err);
          });

          try {
            chunkWriter = this.#file.claimChunk(this.#currentChunk++);
          } catch (e) {
            // chunk already processed
            return;
          }
        }

        buf.copy(buffer, length, offset, buf.length - offset);
        length += buf.length - offset;
      }

      // sync resolve, this makes sure the reader doesn't our run the streamer
      chunkWriter
        .resolve(Buffer.from(buffer.subarray(0, length)))
        .catch((err) => {
          console.error("Error writing chunk", err);
        });
    } catch (e) {
      console.error(e);
    }
  }
}

export abstract class Session {
  // the session type
  // - primary: watching & caching
  // - passthrough: watching & passthrough
  // - passive: fake download
  #type: SessionType;
  #id: string;

  get id() {
    return this.#id;
  }

  get type() {
    return this.#type;
  }

  constructor(id: string, type: SessionType) {
    this.#id = id;
    this.#type = type;
  }

  abstract getUrl(): Promise<string | null> | string | null;
  abstract getHeaders(): Headers;
  abstract read(bytes: number): Readable;
}

export abstract class CacheSession extends Session {
  private file?: FileHandle;
  #mediaSource: MediaSource;

  get mediaSource() {
    return this.#mediaSource;
  }

  constructor(id: string, type: "primary" | "passive", mediaSourceId: string) {
    const mediaSource = getMediaSource(mediaSourceId);
    if (!mediaSource) throw new Error("media not found");
    super(id, type);
    this.#mediaSource = mediaSource;
    try {
      this.file = FILES.open(mediaSourceId);
    } catch (e) {
      console.error(e);
      this.file = undefined;
    }
  }

  async *readChunks(bytes: number) {
    if (!this.file) throw new Error("File not found");
    const count = this.file.data.chunkCount;
    const startChunk = Math.floor(bytes / CHUNK_SIZE);
    for (let i = startChunk; i < count; i++) {
      const chunk = this.file.data.readChunk(i);

      // the chunk doesn't exist, hint the upstream streamer to stream
      if (chunk.unavailable) {
        const url = await this.getUrl();
        // end the stream
        if (!url) return;
        console.log(url);
        const streamer = new Streamer(
          new URL(url, config.upstream.url),
          this.getHeaders(),
          i,
          this.file.data,
          this.type === "passive" ? 1 : 0,
        );

        // TODO: client streams faster than Streamer. which will cause a unclaimed chunk to reach first.
        streamer.start();
      }

      // special case for first chunk
      if (i === startChunk) {
        const buffer = await chunk.read();
        const offset = bytes - startChunk * CHUNK_SIZE;
        yield buffer.subarray(offset);
        continue;
      }

      // special case for last chunk
      if (i === count - 1) {
        const buffer = await chunk.read();
        yield buffer.subarray(
          0,
          this.file.data.size - (count - 1) * CHUNK_SIZE,
        );
        continue;
      }

      yield await chunk.read();
    }
  }

  override read(bytes: number): Readable {
    // align to chunk boundary
    const readable = Readable.from(this.readChunks(bytes));
    readable.addListener("error", (e) => {
      console.log("ERROR!", e);
    });
    readable.addListener("close", () => {
      console.log("CLOSED!");
    });
    readable.addListener("end", () => {
      console.log("END!");
    });
    return readable;
  }
}

export class PrimarySession extends CacheSession {
  private streamUrl: string;
  private headers: Headers;

  constructor(
    url: string,
    headers: Headers,
    playSessionId: string,
    mediaSourceId: string,
  ) {
    super(playSessionId, "primary", mediaSourceId);
    this.headers = cleanUpRequestHeaders(headers);
    this.streamUrl = url;
  }

  override getUrl() {
    return this.streamUrl;
  }

  override getHeaders() {
    return this.headers;
  }
}

export class PassiveSession extends CacheSession {
  private apiKey: string;
  private itemId: string;
  private mediaSourceId: string;
  private headers: Headers;

  constructor(
    itemId: string,
    mediaSourceId: string,
    headers: Headers,
    apiKey: string,
  ) {
    super(randomUUID(), "passive", mediaSourceId);
    this.headers = cleanUpRequestHeaders(headers);
    this.mediaSourceId = mediaSourceId;
    this.itemId = itemId;
    this.apiKey = apiKey;
  }

  async getUrl() {
    try {
      const requestPath = new URL(
        `${config.upstream.url}/emby/Items/${this.itemId}/PlaybackInfo`,
      );

      requestPath.searchParams.append("api_key", this.apiKey);
      requestPath.searchParams.append("mediaSourceId", this.mediaSourceId);

      const playbackInfo = await fetch(requestPath, {
        method: "POST",
        headers: cleanUpRequestHeaders(this.headers, {
          accept: "application/json",
          "content-type": "application/json",
        }),
        body: PLAYBACK_BODY,
      });

      if (!playbackInfo.ok) return null;
      const playbackInfoJson: any = await playbackInfo.json();
      const mediaSource = playbackInfoJson.MediaSources.find(
        (ms: any) => ms.Id === this.mediaSourceId,
      );

      if (!mediaSource) return null;
      return mediaSource.DirectStreamUrl;
    } catch (e) {
      console.error(e);
      return null;
    }
  }

  override getHeaders() {
    return this.headers;
  }
}
