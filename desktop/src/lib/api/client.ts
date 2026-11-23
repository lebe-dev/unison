import { Channel, invoke } from "@tauri-apps/api/core";
import { createClient, type Client } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { UnisonService } from "./gen/unison/local/v1/local_pb";

/** Base URL of the generated clients. The host never reaches the network, the
 * bridge forwards the path of the request to the daemon. */
const BASE_URL = "http://unison";

/** Largest piece of a base64 string built at once. */
const CHUNK_SIZE = 0x8000;

/** Largest stream id, the bridge takes it as an unsigned 32-bit number. */
const MAX_STREAM_ID = 0xffffffff;

/** Id of the last stream this page opened. */
let lastStreamId = 0;

/** nextStreamId returns the id of a new stream, which names it to the bridge
 * when the page cancels it. */
function nextStreamId(): number {
  lastStreamId = lastStreamId >= MAX_STREAM_ID ? 1 : lastStreamId + 1;
  return lastStreamId;
}

/** One unary response of the bridge. */
interface ApiResponse {
  status: number;
  body: string;
}

/** One chunk of a streaming response of the bridge. */
interface StreamChunk {
  chunk: string;
}

/** Target of a call: the service and the method of the local API. */
interface Procedure {
  service: string;
  method: string;
}

export type DaemonClient = Client<typeof UnisonService>;

function encodeBase64(bytes: Uint8Array): string {
  let binary = "";
  for (let offset = 0; offset < bytes.length; offset += CHUNK_SIZE) {
    binary += String.fromCharCode(...bytes.subarray(offset, offset + CHUNK_SIZE));
  }
  return btoa(binary);
}

function decodeBase64(value: string): Uint8Array<ArrayBuffer> {
  const binary = atob(value);
  const bytes = new Uint8Array(new ArrayBuffer(binary.length));
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index);
  }
  return bytes;
}

/** procedure splits the path of a request into the service and the method. */
function procedure(url: string): Procedure {
  const path = new URL(url).pathname.replace(/^\//, "");
  const index = path.lastIndexOf("/");
  if (index <= 0 || index === path.length - 1) {
    throw new Error(`unexpected local API path: ${path}`);
  }
  return { service: path.slice(0, index), method: path.slice(index + 1) };
}

/**
 * Queue of the chunks of a streaming response. The bridge pushes chunks as the
 * daemon sends them, so the stream is read before the call finishes.
 */
class ChunkQueue {
  #chunks: Uint8Array[] = [];
  #pending: {
    resolve: (chunk: Uint8Array | null) => void;
    reject: (error: unknown) => void;
  } | null = null;
  #done = false;
  #error: Error | null = null;

  push(chunk: Uint8Array): void {
    if (this.#done) {
      return;
    }
    const pending = this.#pending;
    if (pending) {
      this.#pending = null;
      pending.resolve(chunk);
      return;
    }
    this.#chunks.push(chunk);
  }

  close(): void {
    this.#settle();
  }

  fail(error: unknown): void {
    this.#error = error instanceof Error ? error : new Error(String(error));
    this.#settle();
  }

  stream(): ReadableStream<Uint8Array> {
    return new ReadableStream<Uint8Array>({
      pull: async (controller) => {
        const chunk = await this.#next();
        if (chunk === null) {
          controller.close();
          return;
        }
        controller.enqueue(chunk);
      },
    });
  }

  #settle(): void {
    this.#done = true;
    const pending = this.#pending;
    if (!pending) {
      return;
    }
    this.#pending = null;
    if (this.#error) {
      pending.reject(this.#error);
      return;
    }
    pending.resolve(null);
  }

  #next(): Promise<Uint8Array | null> {
    const head = this.#chunks.shift();
    if (head) {
      return Promise.resolve(head);
    }
    if (this.#error) {
      return Promise.reject(this.#error);
    }
    if (this.#done) {
      return Promise.resolve(null);
    }
    return new Promise((resolve, reject) => {
      this.#pending = { resolve, reject };
    });
  }
}

/**
 * invokeFetch answers the requests of the Connect transport through the Tauri
 * bridge. The request and response bodies stay in the Connect framing, so the
 * bridge never parses protobuf. An aborted stream is cancelled in the bridge
 * too, which closes its connection to the daemon.
 */
export async function invokeFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const request = new Request(input, init);
  const target = procedure(request.url);
  const contentType = request.headers.get("content-type") ?? "application/proto";
  const body = encodeBase64(new Uint8Array(await request.arrayBuffer()));

  if (!contentType.startsWith("application/connect+")) {
    const response = await invoke<ApiResponse>("api_call", { ...target, body });
    const headers = new Headers({
      "content-type": response.status === 200 ? contentType : "application/json",
    });
    return new Response(decodeBase64(response.body), { status: response.status, headers });
  }

  const queue = new ChunkQueue();
  const channel = new Channel<StreamChunk>();
  channel.onmessage = (message) => queue.push(decodeBase64(message.chunk));

  const signal = request.signal;
  if (signal?.aborted) {
    queue.fail(new DOMException("the call was aborted", "AbortError"));
  } else {
    const id = nextStreamId();
    signal?.addEventListener(
      "abort",
      () => {
        queue.fail(new DOMException("the call was aborted", "AbortError"));
        invoke<void>("api_stream_cancel", { id }).catch((error: unknown) => {
          console.warn("cannot cancel the stream of the daemon", error);
        });
      },
      { once: true },
    );
    invoke<void>("api_stream", { id, ...target, body, onChunk: channel }).then(
      () => queue.close(),
      (error: unknown) => queue.fail(error),
    );
  }

  return new Response(queue.stream(), {
    status: 200,
    headers: new Headers({ "content-type": contentType }),
  });
}

/** client of the local API of the daemon. */
export const client: DaemonClient = createClient(
  UnisonService,
  createConnectTransport({
    baseUrl: BASE_URL,
    useBinaryFormat: true,
    fetch: invokeFetch,
  }),
);
