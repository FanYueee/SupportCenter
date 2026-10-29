const { test, expect } = require('@playwright/test');

for (const locale of ['zh-TW', 'en-US']) {
  test.describe(`${locale} save position`, () => {
    test.use({ locale });
    for (const width of [390, 1440]) {
      test(`${width}px: metadata save keeps the viewport in place`, async ({ page }) => {
        await page.setViewportSize({ width, height: 500 });
        const headers = { 'Accept-Language': locale };
        const original = await (await page.request.get('/admin/tickets/1048', { headers })).text();
        const saved = await (await page.request.get('/admin/tickets/1048?ai=solved', { headers: { ...headers, 'HX-Request': 'true' } })).text();
        const longThread = html => html.replace('class="messages">', 'class="messages"><p>' + 'Earlier conversation. '.repeat(800) + '</p>');
        await page.route('**/admin/tickets/1048', route => route.fulfill({ status: 200, contentType: 'text/html', body: longThread(original) }));
        await page.route('**/admin/tickets/1048/update', route => route.fulfill({
          status: 200, contentType: 'text/html', body: longThread(saved), headers: { 'HX-Replace-Url': '/admin/tickets/1048' },
        }));
        await page.goto('/admin/tickets/1048');
        const form = page.locator('.metadata-panel > form');
        await form.locator('[name="status"]').selectOption('solved');
        const button = form.getByRole('button', { name: locale === 'en-US' ? 'Save changes' : '儲存變更' });
        await button.scrollIntoViewIfNeeded();
        await page.evaluate(() => {
          window.savePosition = { before: scrollY, settled: false };
          document.addEventListener('htmx:afterSettle', () => { window.savePosition.settled = true; }, { once: true });
        });
        await button.click();
        await expect.poll(() => page.evaluate(() => window.savePosition.settled)).toBe(true);
        await expect(page.locator('#detail-status .badge-solved')).toHaveCount(1);
        await expect(form.locator('input[name="version"]')).toHaveValue('2');
        const position = await page.evaluate(() => ({ before: window.savePosition.before, after: scrollY }));
        expect(Math.abs(position.after - position.before), JSON.stringify(position)).toBeLessThan(3);
        await expect(page.locator('html')).not.toHaveClass(/preserving-scroll/);
        await expect(form.getByRole('button')).toBeEnabled();
      });
    }
  });
}

test('a stale save shows the error without scrolling to the top', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 500 });
  await page.goto('/admin/tickets/1048');
  await page.route('**/admin/tickets/1048/update', route => route.fulfill({
    status: 409, contentType: 'text/html', body: '<main class="login-card"><p>客服單已被更新，請重新載入後再試。</p></main>',
  }));
  const button = page.getByRole('button', { name: '儲存變更' });
  await button.scrollIntoViewIfNeeded();
  const before = await page.evaluate(() => scrollY);
  await button.click();
  await expect(page.locator('#request-error')).toContainText('客服單已被更新');
  expect(Math.abs((await page.evaluate(() => scrollY)) - before)).toBeLessThan(3);
  await expect(page.locator('html')).not.toHaveClass(/preserving-scroll/);
  await expect(button).toBeEnabled();
});
