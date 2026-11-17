import { mount } from "svelte";
import App from "./App.svelte";
import { getLocale } from "./lib/i18n";
import { markPlatform } from "./lib/platform";
import { theme } from "./lib/theme.svelte";
import "./app.css";

const target = document.getElementById("app");
if (!target) {
  throw new Error("app root is missing");
}

document.documentElement.lang = getLocale();
markPlatform();
theme.start();
mount(App, { target });
