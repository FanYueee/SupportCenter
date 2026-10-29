const { test, expect } = require('@playwright/test');
const fs = require('node:fs');

for (const locale of ['zh-TW', 'en-US']) {
  test.describe(`${locale} AI ticket flow`, () => {
    test.use({ locale });
    for (const width of [320, 390, 1440]) {
      test(`${width}px: people align and AI actions are distinct`, async ({ page }) => {
        await page.setViewportSize({ width, height: 900 });
        await page.goto('/admin/tickets');
        for (const people of await page.locator('.ticket-people').all()) {
          const groups = people.locator('.ticket-person-group');
          const owner = await groups.nth(0).locator('.ticket-person').boundingBox();
          const staff = await groups.nth(1).locator('.ticket-person-list').boundingBox();
          expect(Math.abs(owner.x - staff.x)).toBeLessThan(1);
        }

        await page.goto('/tickets/1048?ai=processing');
        await expect(page.locator('#detail-status')).toContainText(locale === 'en-US' ? 'AI processing' : 'AI 處理中');
        await expect(page.locator('form[action$="/resolve"]')).toHaveCount(0);
        await page.goto('/tickets/1048?ai=replied');
        await expect(page.locator('#detail-status')).toContainText(locale === 'en-US' ? 'Awaiting your confirmation' : '待您確認');
        const resolve = page.locator('form[action$="/resolve"] button');
        const handoff = page.locator('.handoff-action');
        await expect(resolve).toBeVisible();
        await expect(handoff).toBeVisible();
        const colors = await Promise.all([resolve, handoff].map(button => button.evaluate(element => getComputedStyle(element).backgroundColor)));
        expect(colors[0]).not.toEqual(colors[1]);
        await expect(page.locator('.assistance-panel')).toContainText(locale === 'en-US' ? 'Further replies will be handled by AI.' : '繼續回覆會由 AI 接續處理。');
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
        fs.mkdirSync('artifacts', { recursive: true });
        await page.screenshot({ path: `artifacts/ai-confirmation-${locale}-${width}.png`, fullPage: true });
        await page.emulateMedia({ colorScheme: 'dark' });
        await expect(handoff).toHaveCSS('background-color', 'rgb(48, 39, 22)');
        await expect(resolve).toHaveCSS('background-color', 'rgb(29, 48, 36)');
        await page.goto('/tickets/1048?ai=human');
        await expect(page.locator('form[action$="/resolve"]')).toHaveCount(0);
        await expect(page.locator('.handoff-action')).toHaveCount(0);
      });
    }
    for (const width of [320, 1440]) {
      test(`${width}px: confirming resolution preserves the whole ticket layout`, async ({ page }) => {
        await page.setViewportSize({ width, height: 900 });
        await page.goto('/tickets/1048?ai=replied');
        const response = await page.request.get('/tickets/1048?ai=solved', { headers: { 'HX-Request': 'true', 'Accept-Language': locale } });
        const html = await response.text();
        await page.route('**/tickets/1048/resolve', async route => {
          expect(route.request().method()).toBe('POST');
          const form = new URLSearchParams(route.request().postData());
          expect(form.get('csrf')).toBeTruthy();
          expect(form.get('version')).toBe('1');
          await route.fulfill({ status: 200, contentType: 'text/html', headers: { 'HX-Replace-Url': '/tickets/1048' }, body: html });
        });
        await page.locator('form[action$="/resolve"] button').click();
        await expect(page.locator('#workspace')).toHaveCount(1);
        await expect(page.locator('#workspace > .detail-layout')).toHaveCount(1);
        await expect(page.locator('#detail-status')).toContainText(locale === 'en-US' ? 'Resolved' : '已解決');
        await expect(page.locator('#workspace #ticket-sidebar')).toHaveCount(1);
        await expect(page.locator('#workspace .reply-form')).toBeVisible();
        await expect(page.locator('.auto-close-notice')).toBeVisible();
        await expect(page.locator('form[action$="/resolve"]')).toHaveCount(0);
        await expect(page.locator('#ticket-update-version')).toHaveValue('2');
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
        fs.mkdirSync('artifacts', { recursive: true });
        await page.screenshot({ path: `artifacts/ai-resolved-${locale}-${width}.png`, fullPage: true });
      });
    }
  });
}

test('AI polling updates status and confirmation without replacing a reply draft', async ({ page }) => {
  await page.goto('/tickets/1048?ai=processing');
  await page.locator('textarea[name="body"]').fill('這是還沒送出的回覆');
  const response = await page.request.get('/tickets/1048?ai=replied', { headers: { 'HX-Request': 'true' } });
  const html = (await response.text()).replace(/(name="version"[^>]*value=")1"/g, (_, prefix) => `${prefix}2"`);
  await page.route('**/tickets/1048', route => route.fulfill({ status: 200, contentType: 'text/html', body: html }));
  // Trigger the actual HTMX request immediately instead of waiting for the timer.
  await page.locator('#conversation-activity').evaluate(element => element.setAttribute('hx-trigger', 'test-ai-refresh'));
  await page.evaluate(() => {
    const activity = document.querySelector('#conversation-activity');
    htmx.process(activity);
    htmx.trigger(activity, 'test-ai-refresh');
  });
  await expect(page.locator('#detail-status')).toContainText('AI 已回覆');
  await expect(page.locator('form[action$="/resolve"] button')).toBeVisible();
  await expect(page.locator('textarea[name="body"]')).toHaveValue('這是還沒送出的回覆');
  await expect(page.locator('#conversation-activity')).toHaveAttribute('hx-trigger', 'ai-inactive');
  await expect(page.locator('#ticket-update-version')).toHaveValue('2');
});
