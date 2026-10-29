const { test, expect } = require('@playwright/test');
const fs = require('node:fs');

for (const locale of ['zh-TW', 'en-US']) {
  test.describe(`${locale} service availability`, () => {
    test.use({ locale });
    for (const width of [320, 1440]) {
      test(`${width}px: notice belongs above the reply form`, async ({ page }) => {
        await page.setViewportSize({ width, height: 900 });
        await page.goto('/tickets/1048?ai=replied&service=off_hours');
        const notice = page.locator('.service-notice');
        await expect(notice).toContainText(locale === 'en-US' ? 'Outside support hours' : '目前非客服服務時間');
        await expect(notice).toContainText('目前不在服務時間內，我們會在恢復服務後回覆。');
        await expect(page.getByText('目前不在服務時間內，我們會在恢復服務後回覆。', { exact: true })).toHaveCount(1);
        const noticeBox = await notice.boundingBox();
        const replyBox = await page.locator('.reply-form').boundingBox();
        expect(noticeBox.y + noticeBox.height).toBeLessThanOrEqual(replyBox.y);
        await expect(page.locator('form[action$="/resolve"] button')).toBeVisible();
        await expect(page.locator('textarea[name="body"]')).toBeVisible();
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
        fs.mkdirSync('artifacts', { recursive: true });
        await page.screenshot({ path: `artifacts/service-notice-${locale}-${width}.png`, fullPage: true });
        await page.goto('/tickets/1048?service=leave');
        await expect(notice).toContainText(locale === 'en-US' ? 'Support is on leave' : '客服休假中');
        await page.goto('/tickets/1048');
        await expect(notice).toHaveCount(0);
        await page.goto('/tickets/1048?ai=solved&service=off_hours');
        await expect(notice).toHaveCount(0);
      });
    }
  });
}
