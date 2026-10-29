const { test, expect } = require('@playwright/test');
const fs = require('node:fs');

for (const locale of ['zh-TW', 'en-US']) {
  test.describe(`${locale} ticket participants`, () => {
    test.use({ locale });
    for (const width of [320, 1440]) {
      test(`${width}px: creation, email-only list and self exit`, async ({ page }) => {
        await page.setViewportSize({ width, height: 900 });
        await page.goto('/tickets/new');
        const addresses = page.getByLabel(locale === 'en-US' ? 'Participants (optional)' : '參與者（選填）');
        await expect(addresses).toBeVisible();
        await expect(addresses).toHaveAttribute('multiple', '');
        await addresses.fill('taylor@example.com, future@example.com');
        expect(await addresses.evaluate(input => input.checkValidity())).toBe(true);
        await addresses.fill('invalid-email');
        expect(await addresses.evaluate(input => input.checkValidity())).toBe(false);
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);

        await page.goto('/tickets/1048?participants=owner');
        const participants = page.locator('.invite-list');
        // Every participant is listed by email, registered or not, with no name or avatar.
        await expect(participants.locator('li strong')).toHaveText(['taylor@example.com', 'future.participant@example.com']);
        await expect(participants).not.toContainText('Taylor Lee');
        await expect(participants.locator('img, .avatar')).toHaveCount(0);
        await expect(participants.getByRole('button', { name: /taylor@example\.com/ })).toHaveCount(1);
        await expect(participants).not.toContainText(locale === 'en-US' ? 'Awaiting registration' : '等待加入');
        await expect(participants.getByRole('button')).toHaveCount(2);
        await expect(page.locator('form[action="/tickets/1048/invite"]')).toBeVisible();
        await expect(page.locator('form[action="/tickets/1048/leave"]')).toHaveCount(0);
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);

        await page.goto('/tickets/1048?participants=member');
        const exit = page.getByRole('button', { name: locale === 'en-US' ? 'Leave ticket' : '退出客服單', exact: true });
        await expect(exit).toBeVisible();
        await expect(page.locator('form[action="/tickets/1048/invite"]')).toHaveCount(0);
        await expect(page.locator('form[action="/tickets/1048/uninvite"]')).toHaveCount(0);
        await expect(participants.getByRole('button')).toHaveCount(1);
        await expect(participants).not.toContainText('Taylor Lee');
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
        fs.mkdirSync('artifacts', { recursive: true });
        await page.screenshot({ path: `artifacts/participants-${locale}-${width}-light.png`, fullPage: true });
        await page.emulateMedia({ colorScheme: 'dark' });
        await page.screenshot({ path: `artifacts/participants-${locale}-${width}-dark.png`, fullPage: true });

        // The preview is read-only; backend integration tests cover the actual
        // mutation and redirect (WebKit cannot fulfil a mocked 303).
        let submitted;
        await page.route('**/tickets/1048/leave', async route => {
          submitted = new URLSearchParams(route.request().postData());
          await route.fulfill({ status: 200, contentType: 'text/html', body: '<!doctype html><title>left</title>' });
        });
        await exit.click();
        await expect(page).toHaveTitle('left');
        expect(submitted.get('csrf')).toBeTruthy();
        expect(submitted.has('email')).toBe(false);
      });
    }
  });
}
