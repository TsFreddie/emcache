import type { Interceptor } from "./types";
import { enableDownload } from "./interceptors/enableDownload";
import { streamSaver } from "./interceptors/streamSaver";
import { itemInterceptor } from "./interceptors/itemInterceptor";
import { jsonPrinter } from "./interceptors/jsonPrinter";

// ============================================
// Interceptor chain - add your interceptors here
// ============================================

export const interceptors: Interceptor[] = [
  jsonPrinter,
  itemInterceptor,
  streamSaver,
  enableDownload,
];
