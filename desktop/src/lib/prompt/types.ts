/** Kind of a question the prompt window asks. */
export type PromptKind = "clipboard" | "files";

/** One question of the prompt window. It never carries the content of an
 * item, only what it is and where it comes from. */
export interface Prompt {
  /** Item ID for the clipboard, transfer ID for files. */
  id: string;
  kind: PromptKind;
  /** Name of the device that sent it. */
  device: string;
  /** Short description: the kind of a clip, the names of the files. */
  summary: string;
  /** Size in the locale the main window picked at startup. */
  size: string;
}

/** Answer of the user. A clipboard prompt is applied or skipped, a files
 * prompt is accepted or declined. "later" hides the window and keeps the
 * questions. */
export type PromptAction = "apply" | "skip" | "accept" | "decline" | "later";

/** Answer the prompt window sends to the main window. */
export interface PromptAnswer {
  id: string;
  action: PromptAction;
}

/** Event of the main window with the questions that wait. */
export const PROMPTS_EVENT = "prompts";
/** Event of the prompt window that asks for the questions after it loads. */
export const PROMPT_READY_EVENT = "prompt-ready";
/** Event of the prompt window with an answer. */
export const PROMPT_ANSWER_EVENT = "prompt-answer";
/** Label of the prompt window. */
export const PROMPT_WINDOW = "prompt";
