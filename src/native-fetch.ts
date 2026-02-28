export type NativeRequestBody = Uint8Array | ArrayBuffer | string | null | undefined;

export type NativeResponseBody = Uint8Array | ArrayBuffer | string | null | undefined;

export interface NativeRequest {
  method: string;
  url: string;
  headers: Record<string, string>;
  body?: NativeRequestBody;
}

export interface NativeResponse {
  status: number;
  statusText?: string;
  headers?: Record<string, string | readonly string[]>;
  body?: NativeResponseBody;
}

export type NativeRequestFn = (request: NativeRequest) => NativeResponse | Promise<NativeResponse>;
export type FetchLike = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;

export interface NapiNativeHTTPModule {
  request: NativeRequestFn;
}

function toArrayBuffer(input: ArrayBufferLike | Uint8Array): ArrayBuffer {
  const view = input instanceof Uint8Array ? input : new Uint8Array(input);
  const copy = new Uint8Array(view.byteLength);
  copy.set(view);
  return copy.buffer;
}

function normalizeBody(body: NativeResponseBody): BodyInit | null {
  if (body == null) {
    return null;
  }
  if (typeof body === "string") {
    return body;
  }
  if (body instanceof Uint8Array) {
    return toArrayBuffer(body);
  }
  return toArrayBuffer(body);
}

function requestHeaders(headers: Headers): Record<string, string> {
  const out: Record<string, string> = {};
  headers.forEach((value, key) => {
    out[key] = value;
  });
  return out;
}

function responseHeaders(headers?: Record<string, string | readonly string[]>): Headers {
  const out = new Headers();
  if (!headers) {
    return out;
  }
  for (const [key, value] of Object.entries(headers)) {
    if (typeof value === "string") {
      out.set(key, value);
      continue;
    }
    for (const v of value) {
      out.append(key, v);
    }
  }
  return out;
}

function abortError(): Error {
  return new DOMException("The operation was aborted", "AbortError");
}

async function readRequestBody(req: Request): Promise<NativeRequestBody> {
  if (req.method === "GET" || req.method === "HEAD") {
    return undefined;
  }
  if (req.body == null) {
    return undefined;
  }
  return new Uint8Array(await req.arrayBuffer());
}

export function createFetchFromNativeRequest(nativeRequest: NativeRequestFn): FetchLike {
  return async function fetchFromNative(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
    const req = new Request(input, init);

    if (req.signal.aborted) {
      throw abortError();
    }

    const payload: NativeRequest = {
      method: req.method,
      url: req.url,
      headers: requestHeaders(req.headers),
      body: await readRequestBody(req),
    };

    const nativeCall = Promise.resolve(nativeRequest(payload));

    const response = req.signal
      ? await Promise.race([
          nativeCall,
          new Promise<never>((_, reject) => {
            req.signal.addEventListener("abort", () => reject(abortError()), { once: true });
          }),
        ])
      : await nativeCall;

    return new Response(normalizeBody(response.body), {
      status: response.status,
      statusText: response.statusText,
      headers: responseHeaders(response.headers),
    });
  };
}

export function createFetchFromNapiModule(module: NapiNativeHTTPModule): FetchLike {
  return createFetchFromNativeRequest(module.request);
}
