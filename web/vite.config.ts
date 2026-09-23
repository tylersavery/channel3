/// <reference types="vitest/config" />
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// The build writes into dist/ui, which is the directory web/embed.go carries
// into the binary. dist/.gitkeep sits beside it and is committed, so emptying
// dist/ui on every build never disturbs it and never dirties the tree.
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist/ui',
    emptyOutDir: true,
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test-setup.ts'],
    // Time formatting is part of what these tests assert, so the zone is
    // pinned rather than inherited from whoever is running them. The test
    // script sets TZ as well, for anyone who runs vitest directly.
    env: { TZ: 'America/Toronto' },
  },
})
