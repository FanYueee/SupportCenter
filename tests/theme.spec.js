const { test, expect } = require('@playwright/test');
const dark = 'rgb(23, 23, 23)';
const light = 'rgb(244, 244, 244)';

for (const locale of ['zh-TW', 'en-US']) {
  test.describe(`${locale} appearance toggle`, () => {
    test.use({ locale, colorScheme: 'light' });
    test('follows the system until selected, then retains the choice across pages', async ({ page }) => {
      await page.setViewportSize({ width: 320, height: 800 });
      const toggle = page.locator('.theme-toggle');
      await page.goto('/tickets');
      await expect(toggle).toBeVisible();
      await expect(toggle).toHaveText('');
      await expect(toggle.locator('.theme-icon-sun')).toBeVisible();
      await expect(toggle.locator('.theme-icon-moon')).toBeHidden();
      await expect(toggle).toHaveAccessibleName(locale === 'en-US' ? 'Switch to dark mode' : '切換至深色模式');
      await page.emulateMedia({ colorScheme: 'dark' });
      await expect(page.locator('body')).toHaveCSS('background-color', dark);
      await expect(toggle.locator('.theme-icon-moon')).toBeVisible();
      await expect(toggle.locator('.theme-icon-sun')).toBeHidden();
      await expect(toggle).toHaveAccessibleName(locale === 'en-US' ? 'Switch to light mode' : '切換至淺色模式');

      await toggle.focus();
      await page.keyboard.press('Enter');
      await expect(page.locator('body')).toHaveCSS('background-color', light);
      await expect(toggle.locator('.theme-icon-sun')).toBeVisible();
      await page.reload();
      await expect(page.locator('body')).toHaveCSS('background-color', light);
      await page.emulateMedia({ colorScheme: 'light' });
      await page.emulateMedia({ colorScheme: 'dark' });
      await expect(page.locator('body')).toHaveCSS('background-color', light);

      // A normal HTMX navigation must retain the same appearance and header control.
      await page.getByRole('link', { name: locale === 'en-US' ? 'Create ticket' : '建立客服單', exact: true }).click();
      await expect(page).toHaveURL('/tickets/new');
      await expect(page.locator('body')).toHaveCSS('background-color', light);
      await toggle.click();
      await expect(page.locator('body')).toHaveCSS('background-color', dark);
      await page.emulateMedia({ colorScheme: 'light' });
      for (const path of ['/admin/tickets', '/login', '/tickets']) {
        await page.goto(path);
        await expect(page.locator('body')).toHaveCSS('background-color', dark);
        await expect(toggle.locator('.theme-icon-moon')).toBeVisible();
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      }
    });
  });
}

test('switching still works when browser storage is unavailable', async ({ page }) => {
  await page.addInitScript(() => {
    Storage.prototype.getItem = () => { throw new DOMException('Blocked', 'SecurityError'); };
    Storage.prototype.setItem = () => { throw new DOMException('Blocked', 'SecurityError'); };
  });
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.emulateMedia({ colorScheme: 'dark' });
  await page.goto('/login');
  await expect(page.locator('body')).toHaveCSS('background-color', dark);
  await page.locator('.theme-toggle').click();
  await expect(page.locator('body')).toHaveCSS('background-color', light);
  await page.locator('.theme-toggle').click();
  await expect(page.locator('body')).toHaveCSS('background-color', dark);
  expect(errors).toEqual([]);
});

test('appearance choice synchronizes with another open tab', async ({ context, page }) => {
  const other = await context.newPage();
  await page.emulateMedia({ colorScheme: 'light' });
  await other.emulateMedia({ colorScheme: 'light' });
  await Promise.all([page.goto('/tickets'), other.goto('/login')]);
  await page.locator('.theme-toggle').click();
  await expect(other.locator('body')).toHaveCSS('background-color', dark);
  await expect(other.locator('.theme-icon-moon')).toBeVisible();
  await other.locator('.theme-toggle').click();
  await expect(page.locator('body')).toHaveCSS('background-color', light);
  await expect(page.locator('.theme-icon-sun')).toBeVisible();
  await other.close();
});

test('without JavaScript the system theme and normal navigation remain available', async ({ browser }) => {
  for (const scheme of ['light', 'dark']) {
    const context = await browser.newContext({ javaScriptEnabled: false, colorScheme: scheme });
    const page = await context.newPage();
    await page.goto('http://127.0.0.1:8081/tickets');
    await expect(page.locator('.theme-toggle')).toBeHidden();
    await expect(page.locator('body')).toHaveCSS('background-color', scheme === 'dark' ? dark : light);
    await page.getByRole('link', { name: /Create ticket|建立客服單/, exact: true }).click();
    await expect(page).toHaveURL(/\/tickets\/new$/);
    await context.close();
  }
});

test('switching appearance animates the icons and reveals the new theme from the switch', async ({ page }) => {
  await page.emulateMedia({ colorScheme: 'light', reducedMotion: 'no-preference' });
  const errors = [];
  page.on('console', m => { if (m.type() === 'error') errors.push(m.text()); });
  await page.goto('/tickets');
  const toggle = page.locator('[data-theme-toggle]');
  await expect(toggle.locator('.theme-icon-sun')).toBeVisible();
  const box = await toggle.boundingBox();
  await toggle.click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  // The circle grows from the centre of the switch.
  const origin = await page.evaluate(() => [getComputedStyle(document.documentElement).getPropertyValue('--theme-x'), getComputedStyle(document.documentElement).getPropertyValue('--theme-y')]);
  expect(Math.abs(parseFloat(origin[0]) - (box.x + box.width / 2))).toBeLessThan(1);
  expect(Math.abs(parseFloat(origin[1]) - (box.y + box.height / 2))).toBeLessThan(1);
  await expect(toggle.locator('.theme-icon-moon')).toBeVisible();
  await expect(toggle.locator('.theme-icon-sun')).toBeHidden();
  await expect(page.locator('html')).not.toHaveClass(/theme-transition/);
  expect(await page.locator('body').evaluate(el => getComputedStyle(el).backgroundColor)).toBe('rgb(23, 23, 23)');
  expect(errors).toEqual([]);
});
