import { mount } from "svelte";
import App from "./App.svelte";
import { markPlatform } from "./lib/platform";
import { theme } from "./lib/theme.svelte";
import "./app.css";

const target = document.getElementById("app");
if (!target) {
  throw new Error("app root is missing");
}

markPlatform();
theme.start();
mount(App, { target });
