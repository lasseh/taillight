import js from '@eslint/js'
import tseslint from 'typescript-eslint'
import pluginVue from 'eslint-plugin-vue'
import configPrettier from 'eslint-config-prettier'
import globals from 'globals'

// Flat config for Vue 3 + TypeScript. Advisory only — NOT wired into the
// blocking `make lint` (type-check) or `make build`. Run with `npm run lint`.
// Uses the non-type-checked tseslint.configs.recommended to avoid projectService
// setup; promote to recommendedTypeChecked in a follow-up once the baseline is clean.
export default tseslint.config(
  {
    ignores: ['dist/**', 'node_modules/**', 'coverage/**', 'src/lib/prism-junos.css'],
  },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  ...pluginVue.configs['flat/recommended'],
  {
    files: ['**/*.{ts,vue}'],
    languageOptions: {
      ecmaVersion: 'latest',
      sourceType: 'module',
      globals: { ...globals.browser },
      // Parse <script lang="ts"> blocks in .vue files with the TS parser.
      parserOptions: { parser: tseslint.parser },
    },
    rules: {
      // TypeScript already resolves identifiers and types (e.g. DOM lib types
      // like ScrollBehavior), so core no-undef only produces false positives here.
      // typescript-eslint recommends disabling it for TS sources.
      'no-undef': 'off',
    },
  },
  {
    // Build/tooling files run in Node.
    files: ['*.{js,ts}', 'vite.config.ts'],
    languageOptions: { globals: { ...globals.node } },
  },
  configPrettier,
)
