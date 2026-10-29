const { defineConfig } = require('@playwright/test');
module.exports = defineConfig({
  testDir: './tests',
  testIgnore: 'acceptance.spec.js',
  use: { baseURL: 'http://127.0.0.1:8081', browserName: 'chromium', locale: 'zh-TW' },
  webServer: { command: 'go run ./cmd/preview', url: 'http://127.0.0.1:8081', reuseExistingServer: !process.env.CI },
});
