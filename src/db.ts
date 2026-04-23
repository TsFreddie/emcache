import { Database } from "bun:sqlite";
import { resolve } from "node:path";
import { config } from "./config";

export const DB = new Database(resolve(config.storage, "./db.sqlite"));

// enable optimizations
DB.run(`PRAGMA journal_mode = WAL;`);
