import { mount } from "svelte";
import PromptWindow from "./lib/prompt/PromptWindow.svelte";
import { theme } from "./lib/theme.svelte";
import "./app.css";

const target = document.getElementById("prompt");
if (!target) {
  throw new Error("prompt root is missing");
}

theme.start();
mount(PromptWindow, { target });
