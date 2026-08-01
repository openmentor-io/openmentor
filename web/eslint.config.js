const globals = require('globals')
const react = require('eslint-plugin-react')
const reactHooks = require('eslint-plugin-react-hooks')
const { fixupPluginRules } = require('@eslint/compat')
const js = require('@eslint/js')
const { FlatCompat } = require('@eslint/eslintrc')
const nextPlugin = require('@next/eslint-plugin-next')
const tseslint = require('typescript-eslint')

const compat = new FlatCompat({
  baseDirectory: __dirname,
  recommendedConfig: js.configs.recommended,
  allConfig: js.configs.all,
})

module.exports = [
  // Applies to every file: the react plugin is enabled globally (via the
  // compat extends below), so its version setting has to be global too or it
  // warns on each non-TS file.
  {
    settings: {
      react: {
        version: 'detect',
      },
    },
  },

  // Base JavaScript recommended config
  js.configs.recommended,

  // TypeScript ESLint recommended configs
  ...tseslint.configs.recommended,

  // React recommended via compat layer
  ...compat.extends('plugin:react/recommended'),

  // Next.js plugin
  {
    plugins: {
      '@next/next': nextPlugin,
    },
    rules: {
      ...nextPlugin.configs.recommended.rules,
    },
  },

  // TypeScript files configuration
  {
    files: ['**/*.ts', '**/*.tsx'],
    languageOptions: {
      ecmaVersion: 'latest',
      sourceType: 'module',
      parser: tseslint.parser,
      parserOptions: {
        project: './tsconfig.json',
        ecmaFeatures: {
          jsx: true,
        },
      },
      globals: {
        ...globals.browser,
        ...globals.node,
      },
    },
    plugins: {
      react,
      'react-hooks': fixupPluginRules(reactHooks),
      '@typescript-eslint': tseslint.plugin,
    },
    settings: {
      react: {
        version: 'detect',
      },
    },
    rules: {
      // Console warnings
      'no-console': ['warn', { allow: ['info', 'warn', 'error'] }],

      // TypeScript specific rules
      '@typescript-eslint/no-unused-vars': ['warn', { vars: 'all', args: 'none', argsIgnorePattern: '^_' }],
      '@typescript-eslint/explicit-function-return-type': 'off',
      '@typescript-eslint/explicit-module-boundary-types': 'off',
      '@typescript-eslint/no-explicit-any': 'warn',
      '@typescript-eslint/no-non-null-assertion': 'warn',

      // React rules
      'react/react-in-jsx-scope': 'off',
      'react/prop-types': 'off',
      'react-hooks/rules-of-hooks': 'error',
      'react-hooks/exhaustive-deps': 'warn',

      // Disable base rule in favor of TypeScript version
      'no-unused-vars': 'off',
    },
  },

  // Node CommonJS tooling that isn't part of the app bundle: build scripts and
  // dotfile configs. Without this they inherit the browser/TS defaults above
  // and every `require`/`module`/`process` reads as an undefined global.
  {
    files: ['scripts/**/*.js', '.prettierrc.js'],
    languageOptions: {
      ecmaVersion: 'latest',
      sourceType: 'commonjs',
      globals: {
        ...globals.node,
      },
    },
    rules: {
      '@typescript-eslint/no-require-imports': 'off',
      // These are CLI tools — printing is the point.
      'no-console': 'off',
    },
  },

  // Ignore patterns
  {
    ignores: [
      'node_modules/**',
      '.next/**',
      'out/**',
      'public/**',
      // Jest/Istanbul output from `npm test -- --coverage`. Its scripts already
      // carry `/* eslint-disable */`, so this isn't about errors — it stops
      // `eslint .` walking hundreds of generated files it can do nothing with.
      'coverage/**',
      '*.config.js',
      '*.config.mjs',
    ],
  },
]
