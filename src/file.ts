import * as fs from "node:fs";
import { platform } from "node:os";
import { BitArray } from "./bitarray";

// hardcoded parameters
export const CHUNK_SIZE = 8 * 1024 * 1024; // 8MB chunks
export const MIN_CHUNK_SAVE_INTERVAL = 10 * 1000; // 10 seconds

export class File {
  private file: number;
  private path: string;
  private fileSize: number;
  private lastChunkSaveTime: number;

  private chunks?: BitArray;
  private claims?: Map<number, Promise<Buffer>>;

  private constructor(path: string) {
    this.file = 0;
    this.path = path;
    this.fileSize = 0;
    this.lastChunkSaveTime = 0;
  }

  static async open(path: string, fileSize: number): Promise<File> {
    const file = new File(path);
    if (fs.existsSync(file.path)) {
      // load finished file
      file.file = fs.openSync(file.path, "r+");
      file.fileSize = fs.fstatSync(file.file).size;

      // only load the file if it is the same size as the expected size
      if (file.fileSize === fileSize) {
        return file;
      }

      fs.closeSync(file.file);
    }

    if (
      fs.existsSync(file.path + ".chunks") &&
      fs.existsSync(file.path + ".progress")
    ) {
      // load in-progress file

      file.chunks = await BitArray.fromFile(file.path + ".chunks");
      file.claims = new Map();
      file.file = fs.openSync(file.path + ".progress", "r+");
      file.fileSize = fs.fstatSync(file.file).size;

      // only load the progress if it is the same size as the expected size
      if (
        file.fileSize === fileSize &&
        file.chunks.size === Math.ceil(file.fileSize / CHUNK_SIZE)
      ) {
        return file;
      }

      fs.closeSync(file.file);
    }

    // create new file
    file.fileSize = fileSize;
    file.file = fs.openSync(file.path + ".progress", "w+");
    fs.ftruncateSync(file.file, file.fileSize);

    // create chunk bitarray
    file.chunks = new BitArray(Math.ceil(file.fileSize / CHUNK_SIZE));
    file.claims = new Map();
    return file;
  }

  async writeChunkState() {
    if (!this.chunks) return;
    await this.chunks.toFile(this.path + ".chunks");
  }

  readFromFile(index: number) {
    return new Promise<Buffer>((resolve, reject) => {
      const position = index * CHUNK_SIZE;
      const expectedSize = Math.min(CHUNK_SIZE, this.fileSize - position);
      fs.read(
        this.file,
        Buffer.alloc(expectedSize),
        0,
        expectedSize,
        position,
        (err, bytesRead, buffer) => {
          if (err) reject(err);
          if (bytesRead !== expectedSize) {
            reject(new Error("Unexpected bytes read"));
          }
          resolve(buffer);
        },
      );
    });
  }

  readChunk(index: number):
    | {
        status: "finished" | "available" | "claimed";
        read: () => Promise<Buffer>;
      }
    | false {
    if (!this.chunks || !this.claims) {
      return {
        status: "finished",
        read: () => this.readFromFile(index),
      };
    }

    // make sure it is not finished or already claimed
    if (this.chunks.get(index)) {
      return {
        status: "available",
        read: () => this.readFromFile(index),
      };
    }

    // reuse claim promise
    const claim = this.claims.get(index);
    if (claim) {
      return {
        status: "claimed",
        read: () => claim,
      };
    }

    return false;
  }

  claimChunk(index: number, promise: Promise<Buffer>) {
    if (!this.chunks || !this.claims) {
      throw new Error("The file has been finalized.");
    }

    if (this.chunks.get(index)) {
      throw new Error("Chunk already finished.");
    }

    if (this.claims.has(index)) {
      throw new Error("Chunk already claimed.");
    }

    this.claims.set(
      index,
      new Promise((resolve, reject) => {
        const position = index * CHUNK_SIZE;
        const expectedSize = Math.min(CHUNK_SIZE, this.fileSize - position);

        promise.then((buffer) => {
          if (buffer.length !== expectedSize) {
            reject(new Error("Unexpected buffer size provided"));
          }

          fs.write(
            this.file,
            buffer,
            0,
            buffer.length,
            position,
            async (err, bytesWritten) => {
              if (err) reject(err);
              if (bytesWritten !== buffer.length) {
                reject(new Error("Unexpected bytes written"));
              }
              this.chunks!.set(index, true);
              this.claims!.delete(index);

              if (this.chunks!.count === this.chunks!.size) {
                // finalize the file because all chunks are written to disk
                this.#finalize();
              } else if (
                this.lastChunkSaveTime >= 0 &&
                Date.now() - this.lastChunkSaveTime > MIN_CHUNK_SAVE_INTERVAL
              ) {
                // block chunk map save while the file is saving
                this.lastChunkSaveTime = -1;
                await this.writeChunkState();
                this.lastChunkSaveTime = Date.now();
              }
              resolve(buffer);
            },
          );
        });
      }),
    );
  }

  #finalize() {
    // check if it is already finalized
    if (!this.chunks || !this.claims) return;

    // clear chunk map
    this.chunks = undefined;
    this.claims = undefined;

    // force a sync write before renaming
    // this way after renaming is done the file is guaranteed to be written to disk
    fs.fdatasyncSync(this.file);

    // rename file
    if (platform() === "win32") {
      // on windows we need to close the file first
      fs.closeSync(this.file);
      fs.renameSync(this.path + ".progress", this.path);
      fs.openSync(this.path, "r+");
    } else {
      // on linux we can rename without worry about locks
      fs.renameSync(this.path + ".progress", this.path);
    }

    // delete chunk map if it exists
    if (fs.existsSync(this.path + ".chunks")) {
      fs.unlinkSync(this.path + ".chunks");
    }
  }
}
