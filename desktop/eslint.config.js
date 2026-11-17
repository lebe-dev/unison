import js from "@eslint/js";
import eslintConfigPrettier from "eslint-config-prettier";
import svelte from "eslint-plugin-svelte";
import globals from "globals";
import tseslint from "typescript-eslint";
import svelteConfig from "./svelte.config.js";

export default tseslint.config(
  {
    ignores: ["dist/", "src/lib/api/gen/", "src-tauri/"],
  },
  js.configs.recommended,
  {
    files: ["**/*.ts", "**/*.svelte", "**/*.svelte.ts"],
    extends: [tseslint.configs.recommendedTypeChecked],
  },
  svelte.configs.recommended,
  svelte.configs.prettier,
  eslintConfigPrettier,
  {
    languageOptions: {
      globals: { ...globals.browser },
      parserOptions: { projectService: true },
    },
  },
  {
    files: ["**/*.svelte", "**/*.svelte.ts", "**/*.svelte.js"],
    languageOptions: {
      parserOptions: {
        projectService: true,
        extraFileExtensions: [".svelte"],
        parser: tseslint.parser,
        svelteConfig,
      },
    },
    rules: {
      // Runes take `let` even when the component never reassigns them.
      "prefer-const": "off",
    },
  },
  {
    files: ["**/*.test.ts"],
    rules: {
      // Tests hand the methods of a mock around as callbacks.
      "@typescript-eslint/unbound-method": "off",
    },
  },
  {
    files: ["eslint.config.js", "svelte.config.js"],
    languageOptions: {
      globals: { ...globals.node },
    },
  },
);
