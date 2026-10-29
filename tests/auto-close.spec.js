const { test, expect } = require('@playwright/test');
const fs = require('node:fs');

for (const locale of ['zh-TW', 'en-US']) {
  test.describe(`${locale} automatic closure`, () => {
    test.use({ locale, timezoneId: 'Asia/Taipei' });
    test('all roles retain their personal notification bell', async ({ page }) => {
      await page.goto('/tickets');
      await expect(page.locator('.notification-link')).toHaveCount(1);
      for (const role of ['support', 'admin']) {
        for (const path of ['/tickets', '/admin/tickets', '/account']) {
          await page.goto(`${path}?role=${role}`);
          await expect(page.locator('.notification-link')).toHaveCount(1);
        }
      }
    });
    for (const width of [320, 1440]) {
      test(`${width}px: manual closure preserves notification history`, async ({ page }) => {
        await page.setViewportSize({ width, height: 900 });
        await page.goto('/notifications?closure=manual');
        const items = page.locator('.notification-item');
        await expect(items).toHaveCount(2);
        await expect(items.first()).toContainText(locale === 'en-US' ? 'Ticket closed' : '客服單已結案');
        await expect(items.nth(1)).toContainText(locale === 'en-US' ? 'Ticket resolved' : '客服單已解決');
        await expect(items.first()).toContainText('#1044');
        await expect(items.nth(1)).toContainText('#1044');
        await expect(page.getByText(locale === 'en-US' ? 'Scheduled closure' : '預計結案')).toHaveCount(0);
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      });
    }
    for (const width of [320, 390, 1440]) {
      test(`${width}px: policy, deadlines and notifications`, async ({ page }) => {
        await page.setViewportSize({ width, height: 900 });
        await page.goto('/admin/automation');
        const days = page.locator('[name="auto_close_days"]');
        await expect(days).toBeVisible();
        await expect(days).toHaveValue('7');
        await expect(days).toHaveAttribute('min', '0');
        await expect(days).toHaveAttribute('max', '365');
        await days.fill('0');
        expect(await days.evaluate(input => input.checkValidity())).toBe(true);
        await days.fill('366');
        expect(await days.evaluate(input => input.checkValidity())).toBe(false);

        await page.goto('/tickets/1044');
        const notice = page.locator('.auto-close-notice');
        await expect(notice).toBeVisible();
        await expect(notice).toContainText(locale === 'en-US' ? 'This ticket is marked as' : '客服單已標記為');
        await expect(notice).toContainText('UTC+8');
        await expect(notice).toContainText(locale === 'en-US' ? 'Reply to this ticket' : '直接回覆');
        await expect(page.locator('textarea[name="body"]')).toBeVisible();
        await expect(page.locator('.notification-link .notification-count')).toHaveText('2');
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);

        await page.goto('/tickets/1045');
        await expect(notice).toContainText(locale === 'en-US' ? 'automatically closed on' : '已於');
        await expect(page.getByRole('button', { name: locale === 'en-US' ? 'Reopen ticket' : '重新開啟客服單' })).toBeVisible();

        await page.locator('.notification-link').click();
        await expect(page).toHaveURL('/notifications');
        await expect(page.locator('.notification-item')).toHaveCount(2);
        await expect(page.locator('.notification-item').first()).toContainText(locale === 'en-US' ? 'Ticket automatically closed' : '客服單已自動結案');
        await expect(page.locator('.notification-item').nth(1)).toContainText(locale === 'en-US' ? 'Scheduled closure' : '預計結案');
        await expect(page.locator('form[action="/notifications/2/read"] input[name="csrf"]')).toHaveCount(1);
        await expect(page.getByRole('button', { name: locale === 'en-US' ? 'Mark all as read' : '全部標為已讀' })).toBeVisible();
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
        fs.mkdirSync('artifacts', { recursive: true });
        await page.screenshot({ path: `artifacts/notifications-${locale}-${width}-light.png`, fullPage: true });
        await page.emulateMedia({ colorScheme: 'dark' });
        await expect(page.locator('.notification-item').first()).toHaveCSS('color', 'rgb(229, 229, 229)');
        await expect(page.locator('.notification-item').first()).toHaveCSS('background-color', 'rgb(32, 32, 32)');
        await page.screenshot({ path: `artifacts/notifications-${locale}-${width}-dark.png`, fullPage: true });
      });
    }
  });
}
