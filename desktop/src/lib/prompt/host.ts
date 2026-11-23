import { invoke } from "@tauri-apps/api/core";
import { emit, listen, type UnlistenFn } from "@tauri-apps/api/event";
import {
  PROMPT_ANSWER_EVENT,
  PROMPT_READY_EVENT,
  PROMPTS_EVENT,
  type Prompt,
  type PromptAnswer,
} from "./types";

/**
 * PromptHost runs in the main window and drives the prompt window: it sends
 * the questions that wait, brings the window up when a new question arrives,
 * hides it when nothing waits and hands the answers back.
 */
export class PromptHost {
  #queue: Prompt[] = [];
  #known = new Set<string>();
  #unlisten: UnlistenFn[] = [];

  constructor(private readonly onAnswer: (answer: PromptAnswer) => void) {}

  /** start listens to the prompt window. */
  async start(): Promise<void> {
    this.#unlisten = await Promise.all([
      listen(PROMPT_READY_EVENT, () => void this.#send()),
      listen<PromptAnswer>(PROMPT_ANSWER_EVENT, (event) => this.#answer(event.payload)),
    ]);
  }

  /** stop releases the listeners. */
  stop(): void {
    for (const unlisten of this.#unlisten) {
      unlisten();
    }
    this.#unlisten = [];
  }

  /**
   * sync shows the questions that wait. A new question brings the window up;
   * a window the user put away stays away until then.
   */
  sync(queue: Prompt[]): void {
    const fresh = queue.some((prompt) => !this.#known.has(prompt.id));
    this.#queue = queue;
    this.#known = new Set(queue.map((prompt) => prompt.id));
    void this.#send();
    if (queue.length === 0) {
      void call("hide_prompt");
      return;
    }
    if (fresh) {
      void call("show_prompt");
    }
  }

  #answer(answer: PromptAnswer): void {
    if (answer.action === "later") {
      void call("hide_prompt");
      return;
    }
    this.onAnswer(answer);
  }

  async #send(): Promise<void> {
    try {
      await emit(PROMPTS_EVENT, this.#queue);
    } catch (error) {
      console.warn("cannot send the questions to the prompt window", error);
    }
  }
}

/** call runs a command of the shell and logs a failure. */
async function call(command: string): Promise<void> {
  try {
    await invoke(command);
  } catch (error) {
    console.warn(`the ${command} command failed`, error);
  }
}
