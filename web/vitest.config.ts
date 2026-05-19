import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';

// Separate from vite.config.ts to keep the build pipeline untouched.
export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
    css: false,
    // userEvent.type fires one input event per keystroke; with v8 coverage
    // instrumentation each handler is meaningfully slower on CI hardware,
    // occasionally pushing the multi-input dialog tests past the default
    // 5s. 15s gives us headroom for the slow case without masking genuinely
    // stuck tests.
    testTimeout: 15000,
    coverage: {
      provider: 'v8',
      reporter: ['text', 'json-summary', 'html'],
      all: true,
      include: ['src/**/*.{ts,tsx}'],
      exclude: [
        'src/api/types.ts',
        'src/vite-env.d.ts',
        'src/**/*.d.ts',
        'src/test/**',
        'src/**/*.test.{ts,tsx}',
        '**/*.config.*',
      ],
      thresholds: {
        perFile: true,
        statements: 90,
        branches: 90,
        functions: 90,
        lines: 90,
      },
    },
  },
});
