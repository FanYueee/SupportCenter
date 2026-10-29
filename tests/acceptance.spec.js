const { test, expect } = require('@playwright/test');
const fs = require('node:fs');

test('real customer and staff workflow with PostgreSQL and private attachments', async ({ browser, baseURL }) => {
  const contexts = [];
  async function login(account) {
    const context = await browser.newContext({ baseURL });
    contexts.push(context);
    const page = await context.newPage();
    await page.goto('/login');
    await page.getByRole('combobox', { name: '驗收身分' }).selectOption(account);
    await page.getByLabel('驗收密碼', { exact: true }).fill(process.env.ACCEPTANCE_PASSWORD);
    await page.getByRole('button', { name: '登入', exact: true }).click();
    await expect(page).toHaveURL(account === 'support' ? /\/admin\/tickets$/ : /\/tickets$/);
    return page;
  }
  try {
    const customer = await login('customer');
    const other = await login('customer2');
    const staff = await login('support');
    const subject = `完整流程驗收 ${Date.now()}`;
    await customer.locator('.page-heading').getByRole('link', { name: '建立客服單', exact: true }).click();
    await customer.getByRole('textbox', { name: /客服單主旨/ }).fill(subject);
    await customer.getByRole('textbox', { name: /問題描述/ }).fill('請協助確認連線問題。');
    await customer.locator('input[name=attachments]').setInputFiles({ name: 'diagnostic.txt', mimeType: 'text/plain', buffer: Buffer.from('private diagnostic') });
    await customer.getByRole('button', { name: '送出客服單', exact: true }).click();
    await expect(customer).toHaveURL(/\/tickets\/\d+$/);
    const path = new URL(customer.url()).pathname;
    await expect(customer.getByRole('heading', { name: subject })).toBeVisible();
    const attachment = customer.getByRole('link', { name: /diagnostic.txt/ });
    const attachmentPath = await attachment.getAttribute('href');
    const downloadEvent = customer.waitForEvent('download');
    await attachment.click();
    const download = await downloadEvent;
    expect(fs.readFileSync(await download.path(), 'utf8')).toBe('private diagnostic');
    expect((await other.request.get(path)).status()).toBe(404);
    expect((await other.request.get(attachmentPath)).status()).toBe(404);
    expect((await other.request.get('/admin/tickets')).status()).toBe(404);

    await staff.goto('/admin/tickets?assigned=unassigned');
    await expect(staff.locator('.ticket-title').filter({ hasText: subject })).toBeVisible();
    await staff.goto(`/admin${path}`);
    await staff.getByRole('combobox', { name: '優先級', exact: true }).selectOption('urgent');
    await staff.getByRole('button', { name: '儲存變更' }).click();
    await expect(staff.locator('.metadata-panel .priority-urgent')).toBeVisible();
    await staff.getByRole('button', { name: '指派自己', exact: true }).click();
    await expect(staff.getByRole('button', { name: '取消指派自己' })).toBeVisible();
    await expect(staff.locator('.metadata-panel dd').filter({ hasText: '驗收客服' })).toBeVisible();
    const savedVersion = await staff.locator('.metadata-panel > form input[name=version]').inputValue();
    await staff.getByRole('button', { name: '儲存變更' }).click();
    await expect(staff.locator('.metadata-panel > form input[name=version]')).toHaveValue(savedVersion);
    await staff.goto('/admin/tickets');
    await staff.getByRole('navigation', { name: '客服單優先級篩選' }).getByRole('link', { name: '緊急' }).click();
    await staff.getByRole('combobox', { name: '負責人篩選' }).selectOption('mine');
    await staff.getByRole('button', { name: '搜尋', exact: true }).click();
    await expect(staff.locator('.ticket-title').filter({ hasText: subject })).toBeVisible();
    await staff.goto(`/admin${path}`);
    await staff.getByRole('textbox', { name: '回覆', exact: true }).fill('內部排查紀錄，不可對外。');
    await staff.getByRole('checkbox', { name: '內部備註（僅客服可見）' }).check();
    await staff.locator('input[name=attachments]').setInputFiles({ name: 'internal.txt', mimeType: 'text/plain', buffer: Buffer.from('internal only') });
    await staff.getByRole('button', { name: '送出回覆' }).click();
    await expect(staff.locator('.internal-note')).toContainText('內部排查紀錄');
    const internalPath = await staff.getByRole('link', { name: /internal.txt/ }).getAttribute('href');
    await customer.reload();
    await expect(customer.locator('.messages')).not.toContainText('內部排查紀錄');
    expect((await customer.request.get(internalPath)).status()).toBe(404);
    await staff.getByRole('textbox', { name: '回覆', exact: true }).fill('請提供錯誤發生時間。');
    await staff.getByRole('button', { name: '送出回覆' }).click();
    await expect(staff.locator('.detail-heading .badge')).toHaveText('等待使用者回覆');
    await customer.reload();
    await expect(customer.locator('.detail-heading .badge')).toHaveText('待您回覆');
    await expect(customer.locator('.messages')).toContainText('請提供錯誤發生時間。');

    const submission = await customer.locator('input[name=submission]').inputValue();
    const csrf = await customer.locator('.reply-form input[name=csrf]').inputValue();
    await customer.getByRole('textbox', { name: '回覆', exact: true }).fill('上午十點發生。');
    await customer.getByRole('button', { name: '送出回覆' }).click();
    await expect(customer.locator('.detail-heading .badge')).toHaveText('處理中');
    const retry = await customer.request.post(`${path}/reply`, {
      headers: { Origin: baseURL, 'HX-Request': 'true' },
      multipart: { csrf, submission, body: '上午十點發生。' },
    });
    expect(retry.status()).toBe(200);
    await customer.reload();
    await expect(customer.locator('.message-body').filter({ hasText: '上午十點發生。' })).toHaveCount(1);
    // Staff page predates the customer's reply: stale metadata must not overwrite it.
    await staff.getByRole('combobox', { name: '更新狀態' }).selectOption('closed');
    await staff.getByRole('button', { name: '儲存變更' }).click();
    await expect(staff.locator('#request-error')).toContainText('客服單已被更新');
    await customer.getByRole('button', { name: '結案', exact: true }).click();
    await expect(customer.getByRole('button', { name: '重新開啟客服單' })).toBeVisible();
    await customer.getByRole('button', { name: '重新開啟客服單' }).click();
    await expect(customer.locator('.detail-heading .badge')).toHaveText('待受理');
    await staff.reload();
    await staff.locator('.event-history summary').click();
    await expect(staff.locator('.event-history')).toContainText('優先級');
    await expect(staff.locator('.event-history')).toContainText('驗收客戶 A');
    await customer.goto('/tickets');
    await customer.getByRole('textbox', { name: '搜尋客服單主旨或編號' }).fill(`#${path.split('/').pop()}`);
    await customer.getByRole('button', { name: '搜尋', exact: true }).click();
    await expect(customer.locator('tbody tr')).toHaveCount(1);
    await expect(customer.locator('.tab-count').first()).toHaveText('1');
    await customer.getByRole('textbox', { name: '搜尋客服單主旨或編號' }).fill(subject);
    await customer.getByRole('button', { name: '搜尋', exact: true }).click();
    await expect(customer.locator('tbody tr')).toHaveCount(1);
    await customer.getByRole('button', { name: '登出' }).click();
    await customer.goto(path);
    await expect(customer).toHaveURL(/\/$/);
  } finally {
    for (const context of contexts) await context.close();
  }
});

test('native customer forms work without JavaScript', async ({ browser, baseURL }) => {
  const context = await browser.newContext({ baseURL, javaScriptEnabled: false });
  try {
    const page = await context.newPage();
    await page.goto('/login');
    await page.getByRole('combobox', { name: '驗收身分' }).selectOption('customer');
    await page.getByLabel('驗收密碼', { exact: true }).fill(process.env.ACCEPTANCE_PASSWORD);
    await page.getByRole('button', { name: '登入', exact: true }).click();
    await expect(page).toHaveURL(/\/tickets$/);

    const subject = `無 JavaScript 驗收 ${Date.now()}`;
    await page.getByRole('link', { name: '建立客服單', exact: true }).click();
    await page.getByRole('textbox', { name: /客服單主旨/ }).fill(subject);
    await page.getByRole('textbox', { name: /問題描述/ }).fill('使用原生表單建立。');
    await page.locator('input[name=attachments]').setInputFiles({ name: 'native.txt', mimeType: 'text/plain', buffer: Buffer.from('native upload') });
    await page.getByRole('button', { name: '送出客服單', exact: true }).click();
    await expect(page).toHaveURL(/\/tickets\/\d+$/);
    await expect(page.getByRole('heading', { name: subject })).toBeVisible();
    await expect(page.getByRole('link', { name: /native.txt/ })).toBeVisible();

    await page.getByRole('textbox', { name: '回覆', exact: true }).fill('使用原生表單回覆。');
    await page.getByRole('button', { name: '送出回覆' }).click();
    await expect(page.locator('.message-body').filter({ hasText: '使用原生表單回覆。' })).toHaveCount(1);
    await page.getByRole('button', { name: '結案', exact: true }).click();
    await expect(page.getByRole('button', { name: '重新開啟客服單' })).toBeVisible();
    await page.getByRole('button', { name: '重新開啟客服單' }).click();
    await expect(page.locator('.detail-heading .badge')).toHaveText('待受理');
    await page.getByRole('button', { name: '登出' }).click();
    await expect(page).toHaveURL(/\/$/);
  } finally {
    await context.close();
  }
});
