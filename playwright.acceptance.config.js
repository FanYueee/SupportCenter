const { defineConfig } = require('@playwright/test');
const fs = require('node:fs');
// Read generated data without evaluating it as shell or JavaScript.
const settings = {};
if (fs.existsSync('.env.acceptance')) {
  for (const line of fs.readFileSync('.env.acceptance', 'utf8').split('\n')) {
    const i = line.indexOf('=');
    if (i > 0) settings[line.slice(0, i)] = line.slice(i + 1);
  }
}
process.env.ACCEPTANCE_URL ||= settings.APP_URL;
process.env.ACCEPTANCE_PASSWORD ||= settings.ACCEPTANCE_PASSWORD;
if (!process.env.ACCEPTANCE_URL || !process.env.ACCEPTANCE_PASSWORD ||
    !process.env.ACCEPTANCE_URL.startsWith('https://') ||
    process.env.ACCEPTANCE_URL === 'https://acceptance.invalid') {
  throw new Error('Configure an HTTPS APP_URL in .env.acceptance and start the acceptance app first.');
}
module.exports = defineConfig({
  testDir: './tests',
  testMatch: 'acceptance.spec.js',
  workers: 1,
  timeout: 120000,
  use: { baseURL: process.env.ACCEPTANCE_URL, browserName: 'chromium', trace: 'off' },
});
