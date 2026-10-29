const { test, expect } = require('@playwright/test');
const fs = require('node:fs');
test('footer links to AtomRoute without a link underline', async ({ page }) => {
  for (const path of ['/login', '/tickets', '/account', '/admin/users']) {
    await page.goto(path);
    const footer = page.locator('.site-footer a');
    await expect(footer).toHaveAttribute('href', 'https://www.atomroute.com/zh');
    await expect(footer).toContainText(/© \d{4} AtomRoute/);
    await expect(footer).toHaveCSS('text-decoration-line', 'none');
  }
});

test('account settings and admin users stay readable on mobile', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 720 });
  await page.goto('/account');
  await expect(page.getByRole('heading', { name: '個人設定' })).toBeVisible();
  await expect(page.locator('[name="avatar_source"]')).toHaveCount(0);
  await expect(page.getByRole('heading', { name: '簽名檔' })).toHaveCount(0);
  await expect(page.getByRole('heading', { name: '登入紀錄' })).toBeVisible();
  await expect(page.getByRole('heading', { name: '使用紀錄' })).toBeVisible();
  await expect(page.locator('.history-source').first()).toContainText('IP 203.0.113.10');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.goto('/admin/users');
  await expect(page.getByRole('heading', { name: '使用者' })).toBeVisible();
  await expect(page.locator('.users-table tbody tr')).toHaveCount(2);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.goto('/admin/account');
  await expect(page.getByRole('heading', { name: '簽名檔' })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('staff list filters have matching frames', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/tickets');
  const controls = [page.locator('.assignment-filter'), page.locator('.sort-filter'), page.locator('.search')];
  const boxes = await Promise.all(controls.map(control => control.boundingBox()));
  for (const box of boxes.slice(1)) {
    expect(Math.abs(box.width - boxes[0].width)).toBeLessThan(1);
    expect(Math.abs(box.height - boxes[0].height)).toBeLessThan(1);
    expect(Math.abs(box.y - boxes[0].y)).toBeLessThan(1);
  }
  await page.setViewportSize({ width: 390, height: 844 });
  await page.reload();
  const mobile = await Promise.all(controls.map(control => control.boundingBox()));
  expect(Math.abs(mobile[0].width - mobile[1].width)).toBeLessThan(1);
  for (const box of mobile.slice(1)) expect(Math.abs(box.height - mobile[0].height)).toBeLessThan(1);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('automatic reply settings are clear on mobile', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 720 });
  await page.goto('/admin/automation');
  await expect(page.getByRole('heading', { name: '自動回覆' })).toBeVisible();
  await expect(page.getByRole('heading', { name: '服務時間', exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { name: '臨時休假' })).toBeVisible();
  await expect(page.locator('input[name="leave_start"]')).toBeVisible();
  await expect(page.locator('input[name="leave_end"]')).toBeVisible();
  await expect(page.locator('textarea[name="leave_body"]')).toBeVisible();
  await expect(page.locator('.day-schedule')).toHaveCount(7);
  await expect(page.locator('.day-schedule').first().getByRole('checkbox', { name: '服務日' })).toBeChecked();
  await expect(page.getByRole('checkbox', { name: '啟用自動回覆' })).toHaveCount(3);
  const monday = page.locator('.day-schedule').first();
  await expect(monday.locator('.range-row')).toHaveCount(2);
  await expect(monday.locator('input[name="day_start_0"]').first()).toHaveValue('09:00');
  await expect(monday.locator('input[name="day_start_0"]').nth(1)).toHaveValue('13:00');
  const row = monday.locator('.range-row').first();
  const inputBox = await row.locator('input[name="day_start_0"]').boundingBox();
  const endBox = await row.locator('input[name="day_end_0"]').boundingBox();
  const removeBox = await row.getByRole('button', { name: '移除時段' }).boundingBox();
  // Narrow phones keep both times readable and move the remove action below the pair.
  expect(inputBox.width).toBeGreaterThanOrEqual(110);
  expect(removeBox.y).toBeGreaterThan(inputBox.y + inputBox.height);
  expect(Math.abs((removeBox.x + removeBox.width) - (endBox.x + endBox.width))).toBeLessThan(2);
  await monday.getByRole('button', { name: '新增時段' }).click();
  await expect(monday.locator('.range-row')).toHaveCount(3);
  await monday.locator('.range-row').last().getByRole('button', { name: '移除時段' }).click();
  await expect(monday.locator('.range-row')).toHaveCount(2);
  await page.locator('.day-schedule').nth(1).locator('summary').click();
  await expect(page.locator('.day-schedule').nth(1).locator('input[name="day_start_1"]')).toHaveValue('09:00');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('admin settings share one stable menu and department prompts fit mobile', async ({ page }) => {
  for (const width of [320, 390, 768, 1440]) {
    await page.setViewportSize({ width, height: 844 });
    const menuPositions = [];
    for (const path of ['/admin/tickets', '/admin/ai', '/admin/automation', '/admin/discord', '/admin/email', '/admin/users', '/admin/departments', '/admin/announcements']) {
      await page.goto(path);
      const menu = page.getByRole('navigation', { name: '主選單' });
      await expect(menu.getByRole('link')).toHaveCount(3);
      menuPositions.push((await menu.getByRole('link', { name: '管理設定' }).boundingBox()).x);
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      if (path !== '/admin/tickets') await expect(page.getByRole('navigation', { name: '管理設定' }).getByRole('link')).toHaveCount(7);
    }
    expect(Math.max(...menuPositions) - Math.min(...menuPositions)).toBeLessThan(2);
    await page.goto('/admin/ai');
    await expect(page.getByRole('heading', { name: 'AI 指令' })).toBeVisible();
    await expect(page.getByText('gpt-6-luna')).toBeVisible();
    for (const name of ['general_prompt', 'billing_prompt', 'technical_prompt']) {
      await expect(page.locator(`textarea[name="${name}"]`)).toBeVisible();
    }
  }
});

test('staff assignment uses a self button and still allows other staff', async ({ page }) => {
  for (const width of [320, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto('/admin/tickets/1046');
    const self = page.getByRole('button', { name: '指派自己' });
    const save = page.getByRole('button', { name: '儲存變更' });
    const other = page.getByRole('button', { name: '指派 Morgan' });
    const billing = page.getByRole('button', { name: '指派 Taylor' });
    const general = page.getByRole('button', { name: '指派 Casey' });
    await expect(self).toBeVisible();
    await expect(other).toBeVisible();
    await expect(billing).toBeVisible();
    await expect(general).toBeVisible();
    await expect(page.locator('.assignment-panel input[type="checkbox"]')).toHaveCount(0);
    const [selfBox, saveBox, otherBox] = await Promise.all([self.boundingBox(), save.boundingBox(), other.boundingBox()]);
    expect(Math.abs(selfBox.width - saveBox.width)).toBeLessThan(2);
    expect(Math.abs(selfBox.width - otherBox.width)).toBeLessThan(2);
    expect(otherBox.y - (selfBox.y + selfBox.height)).toBeGreaterThanOrEqual(20);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  }
  await page.goto('/admin/tickets/1046?only_self=1');
  await expect(page.getByRole('button', { name: '指派 Morgan' })).toHaveCount(0);
  await expect(page.getByText('目前沒有其他客服可指派')).toBeVisible();
  await page.goto('/admin/tickets/1048');
  await expect(page.getByRole('button', { name: '取消指派自己' })).toBeVisible();
  await expect(page.locator('.metadata-panel > form input[type="hidden"][name="assignees"][value="2"]')).toHaveCount(1);
});
test('assignment updates the sidebar without moving the conversation', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 650 });
  await page.goto('/admin/tickets/1046');
  const conversation = await page.locator('.conversation-panel').elementHandle();
  await page.route('**/admin/tickets/1046/assign', async route => {
    const html = await (await page.request.get('/admin/tickets/1046')).text();
    await route.fulfill({ status: 200, contentType: 'text/html', body: html.replace('指派自己', '取消指派自己').replace('id="detail-status"', 'id="detail-status" data-swapped="yes"') });
  });
  const button = page.getByRole('button', { name: '指派自己' });
  await button.scrollIntoViewIfNeeded();
  const before = await page.evaluate(() => scrollY);
  await button.click();
  await expect(page.getByRole('button', { name: '取消指派自己' })).toBeVisible();
  await expect(page.locator('#detail-status')).toHaveAttribute('data-swapped', 'yes');
  expect(await conversation.evaluate(el => el.isConnected)).toBe(true);
  expect(Math.abs((await page.evaluate(() => scrollY)) - before)).toBeLessThan(3);
});
test('clicking any ticket list cell opens its ticket', async ({ page }) => {
  for (const [list, detail, cells] of [
    ['/tickets', '/tickets/1048', [1, 2, 3, 4]],
    ['/admin/tickets', '/admin/tickets/1048', [1, 2, 3, 4, 5]],
  ]) {
    for (const cell of cells) {
      await page.goto(list);
      await page.locator('.table-scroll tbody tr').first().locator('td').nth(cell).click();
      await expect(page).toHaveURL(new RegExp(`${detail}$`));
    }
  }
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/tickets');
  await page.locator('.table-scroll tbody tr').first().locator('.ticket-mobile-meta').click();
  await expect(page).toHaveURL(/\/tickets\/1048$/);
  await page.goto('/admin/tickets');
  await page.locator('.table-scroll tbody tr').first().locator('.ticket-person').first().click();
  await expect(page).toHaveURL(/\/admin\/tickets\/1048$/);
});
test('touch-sized ticket lists keep badges and staff readable within the viewport', async ({ browser }) => {
  for (const width of [320, 390, 768, 900]) {
    const context = await browser.newContext({ locale: 'zh-TW', viewport: { width, height: 844 }, isMobile: true, hasTouch: true });
    const page = await context.newPage();
    await page.goto('http://127.0.0.1:8081/tickets');
    expect(await page.evaluate(() => innerWidth === visualViewport.width && document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    if (width <= 760) {
      const clipped = await page.locator('.table-scroll tbody tr .badge').evaluateAll(badges => badges.some(badge => badge.getBoundingClientRect().right > badge.closest('td').getBoundingClientRect().right + 1));
      expect(clipped).toBe(false);
    }
    await page.goto('http://127.0.0.1:8081/admin/tickets');
    expect(await page.evaluate(() => innerWidth === visualViewport.width && document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    const row = page.locator('.admin-ticket-table tbody tr').first();
    const title = await row.locator('td').first().boundingBox();
    const people = await row.locator('.ticket-people-cell').boundingBox();
    expect(width <= 760 ? people.y > title.y : people.x > title.x).toBe(true);
    await row.locator('.ticket-person').first().tap();
    await expect(page).toHaveURL(/\/admin\/tickets\/1048$/);
    await context.close();
  }
});
for (const scheme of ['light', 'dark']) {
  for (const viewport of [{ width: 1440, height: 1050 }, { width: 390, height: 844 }, { width: 320, height: 720 }]) {
    test(`${scheme} ${viewport.width}: responsive layout, theme, and navigation`, async ({ page }) => {
      await page.setViewportSize(viewport);
      await page.emulateMedia({ colorScheme: scheme, reducedMotion: 'reduce' });
      const errors = [];
      const external = [];
      page.on('pageerror', e => errors.push(e.message));
      page.on('console', m => { if (m.type() === 'error') errors.push(m.text()); });
      page.on('request', r => { if (!r.url().startsWith('http://127.0.0.1:8081')) external.push(r.url()); });
      // WebKit screenshots inject a style this site's CSP rightly blocks, so page errors are checked before each shot.
      const shot = async path => {
        expect(errors).toEqual([]);
        await page.screenshot({ path, fullPage: true });
        errors.length = 0;
      };
      await page.goto('/tickets');
      await expect(page.getByRole('heading', { name: /我的客服單/  })).toBeVisible();
      await expect(page.getByRole('link', { name: 'AtomRoute 支援中心' })).toBeVisible();
      const logoURL = await page.locator('.brand-image img').evaluate(el => el.currentSrc);
      expect(logoURL).toContain(viewport.width <= 760 ? 'atomroute-icon.png' : 'atomroute-logo.png');
      await expect(page.getByRole('combobox', { name: '外觀' })).toHaveCount(0);
      await expect(page.locator('tbody tr')).toHaveCount(4);
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      expect(await page.locator('body').evaluate(el => getComputedStyle(el).backgroundColor)).toBe(scheme === 'dark' ? 'rgb(23, 23, 23)' : 'rgb(244, 244, 244)');
      fs.mkdirSync('artifacts', { recursive: true });
      await shot(`artifacts/tickets-${scheme}-${viewport.width}.png`);
      await page.getByRole('navigation', { name: '客服單狀態篩選' }).getByRole('link', { name: '待您回覆' }).click();
      await expect(page.locator('tbody tr')).toHaveCount(1);
      await expect(page).toHaveURL(/status=waiting/);
      await page.getByRole('navigation', { name: '客服單狀態篩選' }).getByRole('link', { name: '全部', exact: true }).click();
      await expect(page.locator('tbody tr')).toHaveCount(4);
      await page.getByRole('textbox', { name: '搜尋客服單主旨或編號' }).fill('不存在的客服單');
      await page.getByRole('button', { name: '搜尋', exact: true }).click();
      await expect(page.getByRole('heading', { name: '沒有符合條件的客服單' })).toBeVisible();
      await page.goto('/tickets/new');
      await expect(page.getByRole('textbox', { name: /客服單主旨/ })).toBeVisible();
      await expect(page.getByRole('textbox', { name: /客服單主旨/ })).toHaveValue('');
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      await shot(`artifacts/new-${scheme}-${viewport.width}.png`);
      await page.goto('/tickets/1048');
      await expect(page.getByRole('heading', { name: '代理連線出現逾時，想確認節點狀態' })).toBeVisible();
      const backLink = page.getByRole('link', { name: '客服單列表' });
      await expect(backLink).toHaveCSS('border-top-width', '1px');
      expect((await backLink.boundingBox()).height).toBeGreaterThanOrEqual(40);
      await expect(page.locator('.message')).toHaveCount(2);
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      await shot(`artifacts/detail-${scheme}-${viewport.width}.png`);
      expect(errors).toEqual([]);
      expect(external).toEqual([]);
    });
  }
}
test('forms and navigation remain available without JavaScript', async ({ browser }) => {
  const context = await browser.newContext({ locale: 'zh-TW', javaScriptEnabled: false, colorScheme: 'dark' });
  const page = await context.newPage();
  await page.goto('http://127.0.0.1:8081/tickets');
  expect(await page.locator('body').evaluate(el => getComputedStyle(el).backgroundColor)).toBe('rgb(23, 23, 23)');
  await page.getByRole('navigation', { name: '客服單狀態篩選' }).getByRole('link', { name: '待您回覆' }).click();
  await expect(page.locator('tbody tr')).toHaveCount(1);
  await page.goto('http://127.0.0.1:8081/tickets/new');
  await expect(page.getByRole('button', { name: '送出客服單' })).toBeVisible();
  await expect(page.locator('.file-picker-display')).toBeHidden();
  await expect(page.locator('input[type="file"]')).toHaveCSS('opacity', '1');
  await page.goto('http://127.0.0.1:8081/admin/tickets');
  await page.getByRole('navigation', { name: '客服單優先級篩選' }).getByRole('link', { name: '緊急' }).click();
  await expect(page.locator('tbody tr')).toHaveCount(1);
  await page.getByRole('navigation', { name: '客服單優先級篩選' }).getByRole('link', { name: '全部優先級' }).click();
  await page.getByRole('combobox', { name: '負責人篩選' }).selectOption('mine');
  await page.getByRole('button', { name: '搜尋', exact: true }).click();
  await expect(page.locator('tbody tr')).toHaveCount(2);
  await page.goto('http://127.0.0.1:8081/account');
  await page.getByRole('button', { name: 'English' }).click();
  await expect(page.locator('html')).toHaveAttribute('lang', 'en');
  await expect(page.getByRole('heading', { name: 'Account settings' })).toBeVisible();
  await context.close();
});

test('appearance follows changes to the system preference', async ({ page }) => {
  const color = () => page.locator('body').evaluate(el => getComputedStyle(el).backgroundColor);
  await page.addInitScript(() => localStorage.setItem('support-theme', 'light'));
  await page.emulateMedia({ colorScheme: 'dark' });
  await page.goto('/tickets');
  expect(await color()).toBe('rgb(23, 23, 23)');
  await expect(page.getByRole('combobox', { name: '外觀' })).toHaveCount(0);
  await expect(page.locator('script[src*="theme.js"]')).toHaveCount(1);
  await page.emulateMedia({ colorScheme: 'light' });
  expect(await color()).toBe('rgb(244, 244, 244)');
  await page.getByRole('link', { name: '建立客服單', exact: true }).click();
  expect(await color()).toBe('rgb(244, 244, 244)');
  await page.emulateMedia({ colorScheme: 'dark' });
  expect(await color()).toBe('rgb(23, 23, 23)');
  await page.goto('/login');
  expect(await color()).toBe('rgb(23, 23, 23)');
});

test('ticket image opens in an accessible in-page viewer', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/tickets/1048');
  await page.locator('.messages').evaluate(container => {
    const link = document.createElement('a');
    link.className = 'image-preview-link';
    link.href = '/assets/atomroute-icon.png';
    link.dataset.imagePreview = '';
    link.dataset.imageName = 'ticket-image.png';
    link.dataset.originalUrl = '/assets/atomroute-logo.png';
    link.textContent = '預覽圖片';
    container.append(link);
  });
  await page.getByRole('link', { name: '預覽圖片' }).click();
  const dialog = page.getByRole('dialog', { name: '圖片預覽' });
  await expect(dialog).toBeVisible();
  await expect(dialog.locator('[data-preview-name]')).toHaveText('ticket-image.png');
  await expect(dialog.locator('[data-preview-image]')).toHaveAttribute('src', /atomroute-icon\.png/);
  await expect(dialog.getByRole('link', { name: '另開圖片' })).toHaveAttribute('href', /atomroute-icon\.png/);
  await expect(dialog.getByRole('link', { name: '下載原圖' })).toHaveAttribute('href', /atomroute-logo\.png/);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  const offsetFromCenter = () => dialog.evaluate(element => {
    const bounds = element.getBoundingClientRect();
    return Math.max(Math.abs(bounds.left + bounds.width / 2 - innerWidth / 2),
      Math.abs(bounds.top + bounds.height / 2 - innerHeight / 2));
  });
  await expect.poll(offsetFromCenter).toBeLessThan(2);
  await page.setViewportSize({ width: 1440, height: 900 });
  await expect.poll(offsetFromCenter).toBeLessThan(2);
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
  await page.getByRole('link', { name: '預覽圖片' }).click();
  await dialog.getByRole('button', { name: '關閉圖片預覽' }).click();
  await expect(dialog).toBeHidden();
});

test('login page loads only the small appearance script', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.goto('/login');
  await expect(page.getByRole('link', { name: '登入', exact: true })).toHaveAttribute('href', '/login');
  await expect(page.locator('.login-copy')).not.toContainText('AtomRoute 帳號');
  await expect(page.locator('.login-action svg')).toBeVisible();
  await expect(page.getByRole('link', { name: '註冊', exact: true })).toHaveAttribute('href', '/signup');
  // The decorative globe never covers the sign-in actions.
  for (const name of ['登入', '註冊']) {
    const box = await page.getByRole('link', { name, exact: true }).boundingBox();
    expect(await page.evaluate(([x, y]) => document.elementFromPoint(x, y).closest('a')?.textContent.trim(), [box.x + box.width / 2, box.y + box.height / 2])).toBe(name);
  }
  await expect(page.locator('script')).toHaveCount(1);
  await expect(page.locator('script[src*="theme.js"]')).toHaveCount(1);
  await expect(page.locator('script[src*="htmx.min.js"], script[src*="app.js"]')).toHaveCount(0);
  await page.goto('/tickets');
  await expect(page.locator('script[src*="htmx.min.js"], script[src*="app.js"]')).toHaveCount(2);
});

test('long ticket and customer names stay within the mobile layout', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 720 });
  await page.goto('/admin/tickets');
  await page.locator('.ticket-title strong').first().evaluate(el => { el.textContent = 'T'.repeat(160); });
  await page.locator('.ticket-person strong').first().evaluate(el => { el.textContent = 'C'.repeat(160); });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  expect(await page.locator('.ticket-person strong').first().evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
  await page.goto('/admin/tickets/1048');
  await page.locator('.detail-heading h1').evaluate(el => { el.textContent = 'T'.repeat(160); });
  await page.locator('.detail-heading p').evaluate(el => { el.append('C'.repeat(160)); });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  expect(await page.locator('.detail-heading p').evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
  await page.locator('.assignment-name').first().evaluate(el => { el.append('S'.repeat(160)); });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

// iOS Safari sizes native date and time fields as content-box with a minimum
// width, pushing them past the card; without the native look they fit.
test('date and time fields fit their cards without native sizing', async ({ page }) => {
  for (const width of [320, 390, 1280]) {
    await page.setViewportSize({ width, height: 844 });
    await page.goto('/admin/automation');
    const fields = page.locator('input[type="datetime-local"], .day-schedule[open] input[type="time"]');
    expect(await fields.count()).toBeGreaterThanOrEqual(4);
    for (const field of await fields.all()) {
      const fit = await field.evaluate(el => {
        const r = el.getBoundingClientRect(), card = el.closest('.account-card').getBoundingClientRect();
        return { appearance: getComputedStyle(el).appearance, inside: r.left >= card.left && r.right <= card.right };
      });
      expect(fit).toEqual({ appearance: 'none', inside: true });
    }
  }
});

test('staff controls, closed tickets, and compact mobile navigation', async ({ page }) => {
  for (const width of [390, 320]) {
    await page.setViewportSize({ width, height: 844 });
    await page.goto('/admin/tickets/1048');
    await expect(page.getByRole('checkbox', { name: '內部備註（僅客服可見）' })).toBeVisible();
    await expect(page.getByRole('combobox', { name: '更新狀態' })).toBeVisible();
    await expect(page.getByRole('button', { name: '儲存變更' })).toBeVisible();
    await expect(page.getByRole('combobox', { name: '外觀' })).toHaveCount(0);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.screenshot({ path: `artifacts/admin-light-${width}.png`, fullPage: true });
  }
  await page.goto('/tickets/1045');
  await expect(page.getByRole('button', { name: '重新開啟客服單' })).toBeVisible();
  await expect(page.getByRole('textbox', { name: '回覆', exact: true })).toHaveCount(0);
  await page.goto('/tickets');
  await expect(page.locator('.stats-grid, .help-card, .sidebar')).toHaveCount(0);
});

test('priority remains readable in the mobile list and ticket details', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  for (const scheme of ['light', 'dark']) {
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto('/tickets');
    const colors = new Set();
    for (const [priority, label] of [['normal', '標準'], ['urgent', '緊急']]) {
      const item = page.locator(`.ticket-mobile-meta .priority-${priority}`).first();
      await expect(item).toBeVisible();
      await expect(item).toHaveText(label);
      colors.add(await item.evaluate(el => getComputedStyle(el).color));
    }
    expect(colors.size).toBe(2);
    await expect(page.locator('.ticket-mobile-meta time').first()).toBeVisible();
    const titleSize = await page.locator('.ticket-title strong').first().evaluate(el => parseFloat(getComputedStyle(el).fontSize));
    expect(titleSize).toBeGreaterThanOrEqual(15);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.goto('/tickets/1048');
    await expect(page.locator('.metadata-panel .priority-normal')).toBeVisible();
    const detailSize = await page.locator('.message-body').first().evaluate(el => parseFloat(getComputedStyle(el).fontSize));
    expect(detailSize).toBeGreaterThanOrEqual(15);
  }
  await page.setViewportSize({ width: 1440, height: 1050 });
  await page.goto('/tickets');
  await expect(page.locator('.ticket-mobile-meta').first()).toBeHidden();
  await expect(page.locator('tbody td:nth-child(4) .priority-normal').first()).toBeVisible();
});

test('new support ticket offers yellow standard and red urgent priority', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  for (const scheme of ['light', 'dark']) {
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto('/tickets/new');
    const category = page.getByRole('combobox', { name: '問題類別' });
    await expect(category.locator('option')).toHaveText(['一般支援', '帳務支援', '技術支援']);
    await expect(category).toHaveValue('other');
    const select = page.getByRole('combobox', { name: '優先級' });
    await expect(select.locator('option')).toHaveText(['標準', '緊急']);
    await expect(select).toHaveValue('normal');
    const standard = await select.evaluate(el => ({ color: getComputedStyle(el).color, border: getComputedStyle(el).borderTopColor, background: getComputedStyle(el).backgroundColor }));
    expect(standard.color).toBe(standard.border);
    await select.selectOption('urgent');
    const urgent = await select.evaluate(el => ({ color: getComputedStyle(el).color, border: getComputedStyle(el).borderTopColor, background: getComputedStyle(el).backgroundColor }));
    expect(urgent.color).toBe(urgent.border);
    expect(urgent.color).not.toBe(standard.color);
    expect(urgent.background).not.toBe(standard.background);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  }
});

test('ticket details show category icons and colored priority backgrounds', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  for (const scheme of ['light', 'dark']) {
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto('/tickets/1048');
    const category = page.locator('.metadata-panel .category-value');
    await expect(category).toHaveText('技術支援');
    await expect(category.locator('svg')).toBeVisible();
    await expect(page.locator('.metadata-panel .ticket-info-id')).toHaveText('#1048');
    const metadata = page.locator('.metadata-panel dl');
    await expect(metadata.locator('dt')).toContainText(['客服單編號', '問題類別', '優先級', '負責客服', '建立時間', '最近更新']);
    await expect(metadata.locator('dt').filter({ hasText: '建立時間' }).locator('+ dd time')).toHaveAttribute('datetime', /2026-09-26T09:42:00/);
    await page.goto('/tickets/1047');
    const standard = page.locator('.metadata-panel .priority-normal');
    await expect(standard).toHaveText('標準');
    await expect(standard).toHaveCSS('gap', 'normal');
    expect(await standard.evaluate(el => getComputedStyle(el, '::before').content)).toBe('none');
    const standardBackground = await standard.evaluate(el => getComputedStyle(el).backgroundColor);
    expect(standardBackground).not.toBe(await page.locator('body').evaluate(el => getComputedStyle(el).backgroundColor));
    await page.goto('/tickets/1043');
    await expect(page.locator('.metadata-panel .category-value')).toHaveText('一般支援');
    const urgent = page.locator('.metadata-panel .priority-urgent');
    await expect(urgent).toHaveText('緊急');
    expect(await urgent.evaluate(el => getComputedStyle(el).backgroundColor)).not.toBe(standardBackground);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  }
});

test('closing a ticket uses a green action and closed status uses a check icon', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  for (const scheme of ['light', 'dark']) {
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto('/tickets/1048');
    const close = page.getByRole('button', { name: '結案', exact: true });
    await expect(close.locator('svg')).toBeVisible();
    const actionColors = await close.evaluate(el => ({ border: getComputedStyle(el).borderTopColor, text: getComputedStyle(el).color, background: getComputedStyle(el).backgroundColor }));
    expect(actionColors.border).toBe(actionColors.text);
    expect(actionColors.background).not.toBe(await page.locator('body').evaluate(el => getComputedStyle(el).backgroundColor));
    await page.goto('/tickets/1045');
    const closed = page.locator('.detail-heading .badge-closed');
    await expect(closed).toHaveText('已結案');
    await expect(closed.locator('svg')).toBeVisible();
    expect(await closed.evaluate(el => getComputedStyle(el).backgroundColor)).not.toBe(await page.locator('body').evaluate(el => getComputedStyle(el).backgroundColor));
    await page.screenshot({ path: `artifacts/closed-${scheme}-390.png`, fullPage: true });
    await page.goto('/tickets');
    await expect(page.locator('tbody .badge-closed svg')).toHaveCount(0);
    await page.getByRole('link', { name: /已完成/ }).click();
    await expect(page.locator('tbody .badge-closed svg')).toHaveCount(1);
  }
});

test('every ticket status uses a framed badge and a distinct icon', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 720 });
  for (const scheme of ['light', 'dark']) {
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto('/admin/tickets');
    const paths = [];
    for (const status of ['new', 'open', 'waiting', 'on_hold', 'solved', 'closed']) {
      if (status === 'solved') await page.getByRole('link', { name: /已完成/ }).click();
      const badge = page.locator(`tbody .badge-${status}`).first();
      await expect(badge).toBeVisible();
      await expect(badge.locator('svg')).toHaveCount(1);
      await expect(badge).toHaveCSS('border-top-width', '1px');
      expect(await badge.evaluate(el => getComputedStyle(el).backgroundColor)).not.toBe(await page.locator('body').evaluate(el => getComputedStyle(el).backgroundColor));
      paths.push(await badge.locator('svg path').first().getAttribute('d'));
    }
    expect(new Set(paths).size).toBe(6);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.goto('/tickets/1043');
    await expect(page.locator('.detail-heading .badge-open')).toHaveText('處理中');
  }
});

test('attachment picker reports chosen files without changing the native input', async ({ page }) => {
  await page.goto('/tickets/new');
  await expect(page.getByText('選擇檔案', { exact: true })).toBeVisible();
  const input = page.locator('input[type="file"]');
  await input.setInputFiles({ name: 'example.pdf', mimeType: 'application/pdf', buffer: Buffer.from('%PDF-1.4\n') });
  await expect(page.locator('[data-file-summary]')).toHaveText('example.pdf');
  await input.setInputFiles([
    { name: 'one.txt', mimeType: 'text/plain', buffer: Buffer.from('one') },
    { name: 'two.txt', mimeType: 'text/plain', buffer: Buffer.from('two') },
  ]);
  await expect(page.locator('[data-file-summary]')).toHaveText('已選擇 2 個檔案');
  await input.setInputFiles([]);
  await expect(page.locator('[data-file-summary]')).toHaveText('未選擇檔案');
  await input.setInputFiles([1, 2, 3, 4].map(n => ({ name: `${n}.txt`, mimeType: 'text/plain', buffer: Buffer.from('ok') })));
  await expect(page.locator('[data-file-summary]')).toHaveText('最多上傳 3 個檔案');
  expect(await input.evaluate(el => el.checkValidity())).toBe(false);
  await input.setInputFiles({ name: 'large.txt', mimeType: 'text/plain', buffer: Buffer.alloc(10 * 1024 * 1024 + 1, 'a') });
  await expect(page.locator('[data-file-summary]')).toHaveText('每個檔案不可超過 10 MB');
  expect(await input.evaluate(el => el.checkValidity())).toBe(false);
  await input.setInputFiles({ name: 'valid.txt', mimeType: 'text/plain', buffer: Buffer.from('ok') });
  expect(await input.evaluate(el => el.checkValidity())).toBe(true);
});

test('selected images are losslessly optimized and previewed as WebP', async ({ page }) => {
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  page.on('console', message => { if (message.type() === 'error') errors.push(message.text()); });
  await page.goto('/tickets/new');
  const input = page.locator('input[name="attachments"]');
  await input.setInputFiles('web/assets/atomroute-logo.png');
  await expect(page.locator('.image-preview img')).toBeVisible({ timeout: 30000 });
  await expect(input).not.toHaveAttribute('data-processing', 'true');
  const details = await input.evaluate(async element => {
    const image = document.querySelector('.image-preview img');
    const bytes = new Uint8Array(await (await fetch(image.src)).arrayBuffer());
    return {
      type: new Blob([bytes]).type,
      signature: String.fromCharCode(...bytes.slice(0, 4)) + String.fromCharCode(...bytes.slice(8, 12)),
      uploadSize: element.files[0].size,
      uploadType: element.files[0].type,
    };
  });
  expect(details.signature).toBe('RIFFWEBP');
  expect(details.uploadSize).toBeLessThanOrEqual(require('node:fs').statSync('web/assets/atomroute-logo.png').size);
  expect(['image/png', 'image/webp']).toContain(details.uploadType);
  expect(errors).toEqual([]);
});

test('staff priority filter keeps status navigation and clears cleanly', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 720 });
  await page.goto('/admin/tickets');
  await expect(page.getByRole('navigation', { name: '客服單狀態篩選' }).getByRole('link', { name: '等待使用者回覆' })).toBeVisible();
  await page.screenshot({ path: 'artifacts/admin-list-320.png', fullPage: true });
  await page.getByRole('navigation', { name: '客服單優先級篩選' }).getByRole('link', { name: '緊急' }).click();
  await expect(page.locator('tbody tr')).toHaveCount(1);
  await expect(page.locator('.tab-count').first()).toHaveText('1');
  await expect(page).toHaveURL(/priority=urgent/);
  await page.getByRole('navigation', { name: '客服單狀態篩選' }).getByRole('link', { name: '處理中' }).click();
  await expect(page.getByRole('heading', { name: '沒有符合條件的客服單' })).toBeVisible();
  await expect(page).toHaveURL(/priority=urgent/);
  await page.getByRole('link', { name: '清除篩選' }).click();
  await expect(page.locator('tbody tr')).toHaveCount(4);
  await expect(page.getByRole('navigation', { name: '客服單優先級篩選' }).getByRole('link', { name: '全部優先級' })).toHaveClass(/active/);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.setViewportSize({ width: 1440, height: 1050 });
  await page.screenshot({ path: 'artifacts/admin-list-1440.png', fullPage: true });
});

test('staff list places ticket first and keeps each person on one line', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/admin/tickets');
  const row = page.locator('tbody tr').first();
  await expect(row.locator('td')).toHaveCount(6);
  const title = await row.locator('td').nth(0).boundingBox();
  const people = await row.locator('.ticket-people-cell').boundingBox();
  expect(people.x).toBeGreaterThan(title.x + title.width - 1);
  for (const group of await row.locator('.ticket-person-group').all()) {
    const label = await group.locator('.ticket-person-label').boundingBox();
    const avatar = await group.locator('.avatar').first().boundingBox();
    const name = await group.locator('.ticket-person strong').first().boundingBox();
    expect(avatar.x).toBeGreaterThan(label.x);
    expect(name.x).toBeGreaterThan(avatar.x);
    expect(Math.abs(label.y + label.height / 2 - avatar.y - avatar.height / 2)).toBeLessThan(3);
  }
  await page.setViewportSize({ width: 320, height: 720 });
  const mobileTitle = await row.locator('td').nth(0).boundingBox();
  const mobilePeople = await row.locator('.ticket-people-cell').boundingBox();
  expect(mobilePeople.y).toBeGreaterThan(mobileTitle.y);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('staff can find own and unassigned tickets without losing other filters', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 720 });
  await page.goto('/admin/tickets');
  const owner = page.getByRole('combobox', { name: '負責人篩選' });
  await owner.selectOption('mine');
  await page.getByRole('button', { name: '搜尋', exact: true }).click();
  await expect(page.locator('tbody tr')).toHaveCount(2);
  await expect(page.locator('.tab-count').first()).toHaveText('2');
  await page.getByRole('navigation', { name: '客服單狀態篩選' }).getByRole('link', { name: '等待使用者回覆' }).click();
  await expect(page.locator('tbody tr')).toHaveCount(1);
  await expect(page).toHaveURL(/assigned=mine/);
  await page.getByRole('combobox', { name: '負責人篩選' }).selectOption('unassigned');
  await page.getByRole('button', { name: '搜尋', exact: true }).click();
  await expect(page.getByRole('heading', { name: '沒有符合條件的客服單' })).toBeVisible();
  await expect(page.locator('.tab-count').first()).toHaveText('2');
  await page.getByRole('link', { name: '清除篩選' }).click();
  await expect(page.locator('tbody tr')).toHaveCount(4);
  await expect(page.getByRole('combobox', { name: '負責人篩選' })).toHaveValue('');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.screenshot({ path: 'artifacts/admin-list-320.png', fullPage: true });
  await page.goto('/tickets');
  await expect(page.getByRole('combobox', { name: '負責人篩選' })).toHaveCount(0);
});

test('ticket number search updates the status counts', async ({ page }) => {
  await page.goto('/tickets');
  await page.getByRole('textbox', { name: '搜尋客服單主旨或編號' }).fill('1048');
  await page.getByRole('button', { name: '搜尋', exact: true }).click();
  await expect(page.locator('tbody tr')).toHaveCount(1);
  await expect(page.locator('.tab-count').first()).toHaveText('1');
  await expect(page.locator('.ticket-id').first()).toHaveText('#1048');
  await page.getByRole('textbox', { name: '搜尋客服單主旨或編號' }).fill('#1048');
  await page.getByRole('button', { name: '搜尋', exact: true }).click();
  await expect(page.locator('tbody tr')).toHaveCount(1);
  await page.getByRole('navigation', { name: '客服單狀態篩選' }).getByRole('link', { name: '待您回覆' }).click();
  await expect(page.getByRole('heading', { name: '沒有符合條件的客服單' })).toBeVisible();
  await expect(page.locator('.tab-count').first()).toHaveText('1');
});

test('times follow the browser timezone, including HTMX updates, with UTC fallback', async ({ browser }) => {
  const local = await browser.newContext({ locale: 'zh-TW', timezoneId: 'Asia/Taipei' });
  const page = await local.newPage();
  await page.goto('http://127.0.0.1:8081/tickets');
  await expect(page.locator('[data-timezone-label]')).toHaveText('本地時間');
  await expect(page.locator('th .timezone')).toHaveCSS('display', 'block');
  await expect(page.locator('.date-cell time').first()).toHaveText('09/28 17:42');
  await page.setViewportSize({ width: 320, height: 720 });
  await expect(page.locator('.ticket-mobile-meta time').first()).toHaveText('09/28 17:42 UTC+8');
  await page.screenshot({ path: 'artifacts/tickets-local-320.png', fullPage: true });
  await page.getByRole('navigation', { name: '客服單狀態篩選' }).getByRole('link', { name: '待您回覆' }).click();
  await expect(page.locator('tbody tr')).toHaveCount(1);
  await expect(page.locator('.ticket-mobile-meta time').first()).toHaveText('09/28 10:42 UTC+8');
  await page.goto('http://127.0.0.1:8081/tickets/1048');
  await expect(page.locator('.detail-heading time')).toHaveText('2026/09/26 17:42 UTC+8');
  await expect(page.locator('.message-header time').first()).toHaveText('09/28 15:42');
  await page.screenshot({ path: 'artifacts/detail-local-320.png', fullPage: true });
  await local.close();

  const fallback = await browser.newContext({ locale: 'zh-TW', timezoneId: 'Asia/Taipei', javaScriptEnabled: false });
  const noJS = await fallback.newPage();
  await noJS.goto('http://127.0.0.1:8081/tickets/1048');
  await expect(noJS.locator('.detail-heading time')).toHaveText('2026/09/26 09:42 UTC');
  await expect(noJS.locator('[data-timezone-label]')).toHaveText('UTC');
  await fallback.close();
});

test('relative message time updates when the page reloads', async ({ page }) => {
  await page.clock.install({ time: new Date('2026-09-28T07:42:15Z') });
  await page.goto('/tickets/1048');
  const relative = page.locator('.relative-time').first();
  await expect(relative).toHaveText('剛剛');
  await page.clock.fastForward(2 * 60 * 60 * 1000);
  await expect(relative).toHaveText('剛剛');
  await page.reload();
  await expect(relative).toHaveText('2 小時前');
});

test('unfinished and completed tickets remain separately filterable', async ({ page }) => {
  await page.goto('/admin/tickets');
  await expect(page.locator('.phase-tab.active')).toContainText('未完成');
  await expect(page.locator('.admin-ticket-table tbody tr')).toHaveCount(4);
  await page.getByRole('link', { name: /已完成/ }).click();
  await expect(page).toHaveURL(/phase=done/);
  await expect(page.locator('.phase-tab.active')).toContainText('已完成');
  await expect(page.locator('.admin-ticket-table tbody tr')).toHaveCount(2);
  await page.getByRole('link', { name: /已結案/ }).click();
  await expect(page).toHaveURL(/status=closed/);
  await expect(page.locator('.admin-ticket-table tbody tr')).toHaveCount(1);
});

test('browser language and flagged account setting cover the ticket flow', async ({ browser }) => {
  const context = await browser.newContext({ locale: 'en-US' });
  const page = await context.newPage();
  await page.goto('http://127.0.0.1:8081/admin/tickets');
  await expect(page.locator('html')).toHaveAttribute('lang', 'en');
  await expect(page.getByRole('heading', { name: 'Support workspace' })).toBeVisible();
  await expect(page.locator('.phase-tab.active')).toContainText('Unfinished');
  await expect(page.locator('.header-actions form[action="/language"]')).toHaveCount(0);
  await page.goto('http://127.0.0.1:8081/account');
  const language = page.locator('.language-settings');
  await expect(language.getByRole('heading', { name: 'Language' })).toBeVisible();
  const english = language.getByRole('button', { name: 'English' });
  const chinese = language.getByRole('button', { name: '繁體中文' });
  await expect(english).toHaveAttribute('aria-pressed', 'true');
  for (const button of [english, chinese]) {
    const flag = await button.locator('svg.flag-icon').boundingBox();
    const label = await button.locator('span').boundingBox();
    expect(flag.x + flag.width).toBeLessThan(label.x);
  }
  await chinese.click();
  await expect(page).toHaveURL('http://127.0.0.1:8081/account');
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-Hant');
  await expect(language.getByRole('heading', { name: '語言' })).toBeVisible();
  await expect(chinese).toHaveAttribute('aria-pressed', 'true');
  await page.goto('http://127.0.0.1:8081/admin/tickets?phase=done');
  await expect(page.locator('.phase-tab.active')).toContainText('已完成');
  await page.goto('http://127.0.0.1:8081/account');
  await page.getByRole('button', { name: 'English' }).click();
  await expect(page.locator('html')).toHaveAttribute('lang', 'en');
  await page.goto('http://127.0.0.1:8081/admin/tickets/1048');
  await expect(page.getByRole('heading', { name: 'Ticket details' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Unassign myself' })).toBeVisible();
  await page.setViewportSize({ width: 320, height: 720 });
  await page.goto('http://127.0.0.1:8081/account');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await context.close();
});

test('English interface text is translated across customer and admin pages', async ({ browser }) => {
  const context = await browser.newContext({ locale: 'en-US' });
  const page = await context.newPage();
  const paths = ['/tickets', '/tickets/new', '/tickets/1048', '/account', '/admin/tickets', '/admin/tickets/1048', '/admin/users', '/admin/ai', '/admin/automation', '/admin/departments', '/admin/discord', '/admin/discord?state=error', '/admin/discord?state=empty', '/admin/email', '/admin/email?state=error', '/admin/email?state=off'];
  for (const path of paths) {
    const response = await page.goto(`http://127.0.0.1:8081${path}`);
    expect(response.headers()['content-language']).toBe('en');
    await expect(page.locator('html')).toHaveAttribute('lang', 'en');
    const untranslated = await page.locator('h1,h2,h3,h4,dt,th,button,summary,.tab,.phase-tab,.priority-tab,.nav-item,.field-hint,.field,.back-link').evaluateAll(elements => elements
      .filter(el => !el.matches('.detail-heading h1,button[value="zh-Hant"]'))
      .map(el => {
        const copy = el.cloneNode(true);
        copy.querySelectorAll('textarea,input').forEach(node => node.remove());
        return copy.textContent.trim().replace(/\s+/g, ' ');
      })
      .filter(text => /[\u3400-\u9fff]/.test(text)));
    expect(untranslated, `Untranslated UI text on ${path}`).toEqual([]);
  }
  await context.close();
});
