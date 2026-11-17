<script lang="ts">
  import Icon from "../Icon.svelte";
  import { onDestroy, onMount } from "svelte";
  import { emit, listen, type UnlistenFn } from "@tauri-apps/api/event";
  import {
    PROMPT_ANSWER_EVENT,
    PROMPT_READY_EVENT,
    PROMPTS_EVENT,
    type Prompt,
    type PromptAction,
    type PromptAnswer,
  } from "./types";

  /** Questions that wait, oldest first. The first one is on screen. */
  let queue = $state<Prompt[]>([]);
  /** Questions answered in this window whose removal has not arrived yet. */
  let answered = $state<string[]>([]);
  let unlisten: UnlistenFn | null = null;

  const waiting = $derived(queue.filter((prompt) => !answered.includes(prompt.id)));
  const current = $derived(waiting[0] ?? null);

  onMount(() => {
    void listen<Prompt[]>(PROMPTS_EVENT, (event) => {
      queue = event.payload;
      // An answer is done once the question left the queue.
      answered = answered.filter((id) => queue.some((prompt) => prompt.id === id));
    }).then((stop) => {
      unlisten = stop;
      // The main window sends the questions once this window listens.
      void emit(PROMPT_READY_EVENT);
    });
  });

  onDestroy(() => {
    unlisten?.();
    unlisten = null;
  });

  function answer(prompt: Prompt, action: PromptAction): void {
    if (action !== "later") {
      answered = [...answered, prompt.id];
    }
    const payload: PromptAnswer = { id: prompt.id, action };
    void emit(PROMPT_ANSWER_EVENT, payload);
  }
</script>

<main class="prompt">
  {#if current}
    <header>
      <span class="app-icon"><Icon name={current.kind === "files" ? "file" : "clipboard"} /></span>
      <span class="title"
        >{current.kind === "files" ? "Files" : "Clipboard"} from {current.device}</span
      >
      {#if waiting.length > 1}
        <span class="count">1 of {waiting.length}</span>
      {/if}
    </header>

    <p class="question">
      {#if current.kind === "files"}
        Accept <strong>{current.summary}</strong>?
      {:else}
        Apply <strong>{current.summary.toLowerCase()}</strong> to the clipboard?
      {/if}
      {#if current.size}
        <span class="size">{current.size}</span>
      {/if}
    </p>

    <div class="actions">
      <button class="button ghost" type="button" onclick={() => answer(current, "later")}
        >Later</button
      >
      {#if current.kind === "files"}
        <button class="button danger" type="button" onclick={() => answer(current, "decline")}
          >Decline</button
        >
        <button class="button primary" type="button" onclick={() => answer(current, "accept")}
          >Accept</button
        >
      {:else}
        <button class="button" type="button" onclick={() => answer(current, "skip")}>Skip</button>
        <button class="button primary" type="button" onclick={() => answer(current, "apply")}
          >Apply</button
        >
      {/if}
    </div>
  {:else}
    <p class="question muted">Nothing waits for an answer.</p>
  {/if}
</main>

<style>
  :global(body) {
    margin: 0;
    overflow: hidden;
    background-color: var(--toolbar);
  }

  .prompt {
    display: flex;
    flex-direction: column;
    gap: 10px;
    box-sizing: border-box;
    height: 100vh;
    padding: 14px 16px;
    color: var(--foreground);
    background-color: var(--toolbar);
  }

  header {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .app-icon {
    display: flex;
    flex: none;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    border-radius: 7px;
    color: #ffffff;
    background-color: var(--accent);
  }

  .title {
    flex: 1;
    overflow: hidden;
    font-weight: 600;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .count {
    flex-shrink: 0;
    color: var(--muted);
    font-size: 11px;
  }

  .question {
    flex: 1;
    margin: 0;
    overflow: hidden;
    overflow-wrap: anywhere;
    color: var(--secondary);
  }

  .size {
    display: block;
    margin-top: 2px;
    color: var(--muted);
    font-size: 12px;
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }
</style>
