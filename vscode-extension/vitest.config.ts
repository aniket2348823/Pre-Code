import { defineConfig } from 'vitest/config';

// Tests must run from the TypeScript sources — the compiled CommonJS copies in
// dist/ are picked up by vitest's default glob otherwise and fail to load
// ("Vitest cannot be imported in a CommonJS module").
export default defineConfig({
    test: {
        include: ['src/**/*.test.ts'],
    },
});
