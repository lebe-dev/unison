import {
  EventKind,
  type Event as DaemonEvent,
  type ShareRequest,
} from "./api/gen/unison/local/v1/local_pb";
import { daemon } from "./daemon.svelte";
import { formatBytes, kindLabel } from "./format";
import { history } from "./history.svelte";
import { translate } from "./i18n";
import type { Prompt, PromptAction } from "./prompt/types";
import { requests } from "./requests.svelte";

/**
 * PromptStore keeps the questions the user has to answer: the clipboard items
 * a device holds for confirmation and the file offers that wait for a
 * decision. The offers come from the requests and the held items from the
 * history, so an answer given on another screen removes the question too.
 */
class PromptStore {
  /** Questions with an answer in flight. */
  answering = $state<string[]>([]);

  /** Clipboard questions by item ID, from the events of the daemon. */
  #clips = $state<Prompt[]>([]);
  #started = false;
  #unsubscribe: (() => void) | null = null;

  /** queue is every question that waits, oldest first. */
  get queue(): Prompt[] {
    const held = this.#clips.filter((prompt) => history.held.includes(prompt.id));
    const files = [...requests.requests].reverse().map(filesPrompt);
    return [...held, ...files];
  }

  /** start follows the events of the daemon. */
  start(): void {
    if (this.#started) {
      return;
    }
    this.#started = true;
    this.#unsubscribe = daemon.onEvent((event) => this.#onEvent(event));
  }

  /** stop releases the subscription. */
  stop(): void {
    this.#started = false;
    this.#unsubscribe?.();
    this.#unsubscribe = null;
  }

  /** answer applies the answer of the user to a question. */
  async answer(id: string, action: PromptAction): Promise<void> {
    if (action === "later" || this.answering.includes(id)) {
      return;
    }
    this.answering = [...this.answering, id];
    try {
      switch (action) {
        case "apply":
          await history.apply(id);
          break;
        case "skip":
          // The item stays in the history; only the question goes away.
          history.release(id);
          break;
        case "accept":
          await requests.decide(id, true);
          break;
        case "decline":
          await requests.decide(id, false);
          break;
      }
    } finally {
      this.answering = this.answering.filter((current) => current !== id);
    }
  }

  #onEvent(event: DaemonEvent): void {
    if (event.kind !== EventKind.CLIP_APPLY || !event.item) {
      return;
    }
    const item = event.item;
    if (this.#clips.some((prompt) => prompt.id === item.id)) {
      return;
    }
    const size = item.representations.reduce((total, part) => total + part.size, 0n);
    this.#clips = [
      ...this.#clips,
      {
        id: item.id,
        kind: "clipboard",
        device: deviceName(event.name, event.deviceId),
        summary: kindLabel(item.kind),
        size: formatBytes(size),
      },
    ];
  }
}

/** filesPrompt describes a file offer. */
function filesPrompt(request: ShareRequest): Prompt {
  const names = request.files.map((file) => file.name).filter((name) => name !== "");
  const count = request.files.length;
  const summary =
    names.length === 1
      ? names[0]
      : names.length > 0
        ? translate("prompt.filesSummaryWithNames", {
            count,
            names: names.slice(0, 3).join(", "),
          })
        : translate("prompt.filesSummary", { count });
  return {
    id: request.id,
    kind: "files",
    device: deviceName(request.deviceName, request.deviceId),
    summary,
    size: formatBytes(request.totalBytes),
  };
}

/** deviceName returns the name of a device, or the name of the paired device
 * with that ID. */
function deviceName(name: string, deviceId: string): string {
  if (name) {
    return name;
  }
  return (
    daemon.devices.find((device) => device.deviceId === deviceId)?.name ||
    translate("common.aDevice")
  );
}

/** prompts is the question state of the app. */
export const prompts = new PromptStore();
