import base from './playwright.config.js';
import { defineConfig } from '@playwright/test';

export default defineConfig({
  ...base,
  use: {
    ...base.use,
    baseURL: process.env.PLAYWRIGHT_BASE_URL || 'http://127.0.0.1:18081',
  },
  webServer: undefined,
});