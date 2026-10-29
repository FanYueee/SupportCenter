const { test, expect } = require('@playwright/test');

for (const locale of ['zh-TW', 'en-US']) {
  test.describe(`${locale} announcements`, () => {
    test.use({ locale });
    for (const scheme of ['light', 'dark']) {
      test(`multiple notices stay above the page and fit mobile in ${scheme} mode`, async ({ page }) => {
        await page.emulateMedia({ colorScheme: scheme });
        for (const width of [320, 390, 1440]) {
          await page.setViewportSize({ width, height: 900 });
          for (const path of ['/tickets', '/tickets/new', '/tickets/1048', '/account', '/admin/tickets']) {
            await page.goto(`${path}?announcements=1&long=1`);
            const banners = page.getByRole('region', { name: locale === 'en-US' ? 'Announcements' : '公告', exact: true });
            await expect(banners.locator('article')).toHaveCount(3);
            const bannerBox = await banners.boundingBox();
            const heading = await page.locator('.page-heading').boundingBox();
            expect(bannerBox.y + bannerBox.height).toBeLessThan(heading.y);
            const styles = await banners.locator('.announcement').evaluateAll(items => items.map(item => ({
              background: getComputedStyle(item).backgroundColor,
              border: getComputedStyle(item).borderLeftColor,
              text: getComputedStyle(item.querySelector('p')).fontSize,
            })));
            expect(new Set(styles.map(style => style.background)).size).toBe(3);
            expect(new Set(styles.map(style => style.border)).size).toBe(3);
            for (const style of styles) expect(parseFloat(style.text)).toBeGreaterThanOrEqual(16);
            await expect(banners.locator('article').first().locator('.announcement-icon circle')).toHaveCount(1);
            await expect(banners.locator('article').nth(1).locator('.announcement-icon circle')).toHaveCount(1);
            await expect(banners.locator('article').nth(2).locator('.announcement-icon circle')).toHaveCount(0);
            await expect(banners.locator('.announcement-copy').first().locator('p')).toHaveCSS('white-space', 'pre-wrap');
            expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
          }
        }
      });
    }

    test('management supports colors, editing, publication and guarded deletion', async ({ page }) => {
      await page.setViewportSize({ width: 320, height: 900 });
      await page.goto('/admin/announcements');
      await expect(page.getByRole('heading', { name: locale === 'en-US' ? 'Announcements' : '公告', exact: true })).toBeVisible();
      const editor = page.locator('.announcement-editor');
      await expect(editor.locator('input[name="title"]')).toHaveAttribute('maxlength', '120');
      await expect(editor.locator('textarea[name="body"]')).toHaveAttribute('maxlength', '2000');
      await expect(editor.getByRole('radio')).toHaveCount(3);
      await expect(editor.locator('.announcement-choice-label')).toHaveText(locale === 'en-US' ? ['Info', 'Success', 'Alert'] : ['資訊', '完成', '重要']);
      await expect(editor.getByRole('radio').first()).toBeChecked();
      await editor.locator('.announcement-color').nth(2).click();
      await expect(editor.getByRole('radio').nth(2)).toBeChecked();
      await expect(editor.getByRole('checkbox')).toBeChecked();
      const manager = page.locator('.announcement-management-list');
      await expect(manager.locator('.announcement-management-item')).toHaveCount(4);
      await expect(manager.locator('.announcement-publication').last()).toHaveText(locale === 'en-US' ? 'Disabled' : '已停用');
      await expect(manager.locator('.announcement-delete-confirm').first()).toBeHidden();
      await manager.locator('.announcement-delete summary').first().click();
      await expect(manager.locator('.announcement-delete-confirm').first()).toBeVisible();
      await expect(manager.locator('.announcement-delete-confirm form').first()).toHaveAttribute('action', '/admin/announcements/3/delete');
      await manager.getByRole('link', { name: locale === 'en-US' ? 'Edit' : '編輯', exact: true }).first().click();
      await expect(page).toHaveURL('/admin/announcements/3');
      await expect(editor.locator('input[name="title"]')).toHaveValue('服務維護通知');
      await expect(editor.locator('form')).toHaveAttribute('action', '/admin/announcements/3/update');
      await expect(editor.locator('input[name="version"]')).toHaveValue('1');
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      await page.goto('/admin/announcements?saved=1');
      await expect(page.locator('.notice.success')).toHaveText(locale === 'en-US' ? 'Saved' : '已儲存');
      await expect(page.locator('.notice.success')).toHaveCSS('background-color', 'rgb(237, 247, 239)');
    });
  });
}

test('announcements follow a manual theme override and do not appear on login', async ({ page }) => {
  await page.emulateMedia({ colorScheme: 'light' });
  await page.goto('/tickets?announcements=1');
  await expect(page.locator('.announcement-blue')).toHaveCSS('background-color', 'rgb(234, 241, 248)');
  await page.locator('.theme-toggle').click();
  await expect(page.locator('.announcement-blue')).toHaveCSS('background-color', 'rgb(28, 44, 60)');
  await page.goto('/login?announcements=1');
  await expect(page.locator('.announcement-banners')).toHaveCount(0);
});
