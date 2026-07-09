import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './tests',
  timeout: 30000,
  use: {
    baseURL: 'http://127.0.0.1:5187',
    trace: 'retain-on-failure',
  },
  webServer: {
    command: 'npm run dev -- --port 5187',
    env: {
      VITE_RED_PANDA_GATEWAY_BASE_URL: process.env.VITE_RED_PANDA_GATEWAY_BASE_URL || '/__red_panda_gateway',
      VITE_RED_PANDA_GATEWAY_PROXY_TARGET: process.env.VITE_RED_PANDA_GATEWAY_PROXY_TARGET || 'http://127.0.0.1:17931',
    },
    url: 'http://127.0.0.1:5187/activity-fixture.html',
    reuseExistingServer: !process.env.CI,
    timeout: 60000,
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
});
