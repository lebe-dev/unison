import { mount } from "svelte";
import App from "./App.svelte";
import { theme } from "./lib/theme.svelte";
import "./app.css";

const target = document.getElementById("app");
if (!target) {
  throw new Error("app root is missing");
}

theme.start();
mount(App, { target });
