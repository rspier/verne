import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: './src/test/setup.js', // if you need setup files
    // you might want to add a css import mock if components import css directly
    // css: true, // or specific mocking configuration
  },
})
