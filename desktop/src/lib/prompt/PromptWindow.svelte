<script lang="ts">
  import Icon from "../Icon.svelte";
  import { onDestroy, onMount } from "svelte";
  import { emit, listen, type UnlistenFn } from "@tauri-apps/api/event";
  import { t } from "../i18n";
  import SasCode from "../pairing/SasCode.svelte";
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
  // The search for friends is on after a Friends pair unless the user turns it
  // off here.
  let friendsDiscovery = $state(true);
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
    if (prompt.kind === "mediated") {
      payload.enableFriendsDiscovery = friendsDiscovery;
    }
    void emit(PROMPT_ANSWER_EVENT, payload);
  }
</script>

<main class="prompt">
  {#if current}
    <header>
      <span class="app-icon"
        ><Icon
          name={current.kind === "files"
            ? "file"
            : current.kind === "mediated"
              ? "add"
              : "clipboard"}
        /></span
      >
      <span class="title">
        {#if current.kind === "files"}
          {t("prompt.filesFrom", { device: current.device })}
        {:else if current.kind === "mediated"}
          {t("prompt.introductionFrom", { device: current.device })}
        {:else}
          {t("prompt.clipboardFrom", { device: current.device })}
        {/if}
      </span>
      {#if waiting.length > 1}
        <span class="count">{t("prompt.countOf", { total: waiting.length })}</span>
      {/if}
    </header>

    {#if current.mediated}
      <div class="question mediated">
        <span class="peer">
          <strong>{current.summary}</strong>
          <span class="muted">{current.mediated.platform} · {current.mediated.deviceId}</span>
        </span>
        <span class="muted">{t("mediated.role")}: {current.mediated.role}</span>
        {#if current.mediated.digits}
          <SasCode
            layout="inline"
            grouped
            digits={current.mediated.digits}
            emoji={current.mediated.emoji}
            ariaLabel={t("mediated.sasAria")}
          />
        {/if}
        {#if current.mediated.alreadyPaired}
          <span class="small">{t("mediated.alreadyPaired")}</span>
        {/if}
        {#if current.mediated.friends}
          <label class="check small">
            <input type="checkbox" bind:checked={friendsDiscovery} />
            {t("mediated.friendsDiscovery")}
          </label>
        {/if}
      </div>
    {:else}
      <p class="question">
        {#if current.kind === "files"}
          {t("prompt.acceptPrefix")} <strong>{current.summary}</strong>{t("prompt.acceptSuffix")}
        {:else}
          {t("prompt.applyPrefix")} <strong>{current.summary}</strong>{t("prompt.applySuffix")}
        {/if}
        {#if current.size}
          <span class="size">{current.size}</span>
        {/if}
      </p>
    {/if}

    <div class="actions">
      <button class="button ghost" type="button" onclick={() => answer(current, "later")}>
        {t("common.later")}
      </button>
      {#if current.kind === "files" || current.kind === "mediated"}
        <button class="button danger" type="button" onclick={() => answer(current, "decline")}>
          {t("common.decline")}
        </button>
        <button class="button primary" type="button" onclick={() => answer(current, "accept")}>
          {t("common.accept")}
        </button>
      {:else}
        <button class="button" type="button" onclick={() => answer(current, "skip")}>
          {t("common.skip")}
        </button>
        <button class="button primary" type="button" onclick={() => answer(current, "apply")}>
          {t("common.apply")}
        </button>
      {/if}
    </div>
  {:else}
    <p class="question muted">{t("prompt.nothingWaits")}</p>
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

  .mediated {
    display: flex;
    flex-direction: column;
    gap: 3px;
    font-size: 12px;
  }

  .peer {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 6px;
  }

  .small {
    font-size: 11px;
    color: var(--muted);
  }

  .check {
    display: flex;
    align-items: flex-start;
    gap: 6px;
    color: var(--secondary);
  }

  .check input {
    margin: 2px 0 0;
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
