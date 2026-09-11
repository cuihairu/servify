import js from '@eslint/js';
import globals from 'globals';
import typescriptParser from '@typescript-eslint/parser';
import typescriptPlugin from '@typescript-eslint/eslint-plugin';

// ESLint 10 flat config，迁移自 .eslintrc.json
export default [
	{
		ignores: ['**/dist/**', '**/node_modules/**'],
	},
	js.configs.recommended,
	{
		files: ['packages/*/src/**/*.{js,ts,tsx}'],
		languageOptions: {
			parser: typescriptParser,
			ecmaVersion: 2020,
			sourceType: 'module',
			globals: {
				...globals.browser,
				...globals.node,
			},
		},
		plugins: {
			'@typescript-eslint': typescriptPlugin,
		},
		rules: {
			...typescriptPlugin.configs.recommended.rules,
			'@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_' }],
			'@typescript-eslint/no-explicit-any': 'warn',
			'@typescript-eslint/explicit-function-return-type': 'off',
			'@typescript-eslint/explicit-module-boundary-types': 'off',
			'prefer-const': 'error',
			'no-console': ['warn', { allow: ['warn', 'error'] }],
		},
	},
	{
		// TS 类型（如 RTCSessionDescriptionInit）由 tsc 校验，no-undef 会误报
		files: ['**/*.ts', '**/*.tsx'],
		rules: {
			'no-undef': 'off',
		},
	},
];
