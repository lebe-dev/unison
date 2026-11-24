/** Kind of a question the prompt window asks. */
export type PromptKind = "clipboard" | "files" | "mediated";

/** Detail of an introduction another device offers. */
export interface MediatedPrompt {
  /** System of the new device, in the locale the main window picked. */
  platform: string;
  /** Short device ID of the new device. */
  deviceId: string;
  /** Role of the new pair, in the locale of the app. */
  role: string;
  /** Six digits of the verification code, grouped for reading. */
  digits: string;
  /** Four emoji of the verification code. */
  emoji: string[];
  /** The new device is paired already and offline. */
  alreadyPaired: boolean;
  /** The new pair is Friends, so the search for friends can be turned on. */
  friends: boolean;
}

/** One question of the prompt window. It never carries the content of an
 * item, only what it is and where it comes from. */
export interface Prompt {
  /** Item ID for the clipboard, transfer ID for files, session ID for an
   * introduction. */
  id: string;
  kind: PromptKind;
  /** Name of the device that sent it, or of the mediator of an introduction. */
  device: string;
  /** Short description: the kind of a clip, the names of the files, the name
   * of the new device. */
  summary: string;
  /** Size in the locale the main window picked at startup. */
  size: string;
  /** Detail of an introduction, absent for the other kinds. */
  mediated?: MediatedPrompt;
}

/** Answer of the user. A clipboard prompt is applied or skipped, a files
 * prompt is accepted or declined, an introduction is accepted or declined.
 * "later" hides the window and keeps the questions. */
export type PromptAction = "apply" | "skip" | "accept" | "decline" | "later";

/** Answer the prompt window sends to the main window. */
export interface PromptAnswer {
  id: string;
  action: PromptAction;
  /** Turn the search for friends on after a Friends pair, set by the switch of
   * an introduction. */
  enableFriendsDiscovery?: boolean;
}

/** Event of the main window with the questions that wait. */
export const PROMPTS_EVENT = "prompts";
/** Event of the prompt window that asks for the questions after it loads. */
export const PROMPT_READY_EVENT = "prompt-ready";
/** Event of the prompt window with an answer. */
export const PROMPT_ANSWER_EVENT = "prompt-answer";
/** Label of the prompt window. */
export const PROMPT_WINDOW = "prompt";
