// Shared types for the proxy

export interface ProxyContext {
  request: Request;
  upstreamUrl: string;
}

// Interceptor functions
export type RequestHandler = (ctx: ProxyContext) => Promise<Response | void> | Response | void;
export type ResponseHandler = (ctx: ProxyContext, response: Response) => Promise<Response> | Response;

/**
 * An interceptor can define request and/or response handlers
 */
export interface Interceptor {
  onRequest?: RequestHandler;
  onResponse?: ResponseHandler;
}
