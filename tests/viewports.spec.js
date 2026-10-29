const { test, expect } = require('@playwright/test');
const fs = require('node:fs');

// Common phone, foldable, tablet, laptop, and desktop screens in both orientations.
const viewports = [
  { name: 'fold-folded', width: 280, height: 653, mobile: true },
  { name: 'iphone-se1', width: 320, height: 568, mobile: true },
  { name: 'android-small', width: 360, height: 780, mobile: true },
  { name: 'iphone-se', width: 375, height: 667, mobile: true },
  { name: 'iphone-15', width: 393, height: 852, mobile: true },
  { name: 'pixel-8', width: 412, height: 915, mobile: true },
  { name: 'iphone-pro-max', width: 430, height: 932, mobile: true },
  { name: 'iphone-se-landscape', width: 667, height: 375, mobile: true },
  { name: 'iphone-15-landscape', width: 852, height: 393, mobile: true },
  { name: 'pixel-landscape', width: 915, height: 412, mobile: true },
  { name: 'android-tablet', width: 600, height: 960, mobile: true },
  { name: 'ipad-mini', width: 744, height: 1133, mobile: true },
  { name: 'ipad', width: 810, height: 1080, mobile: true },
  { name: 'ipad-air', width: 820, height: 1180, mobile: true },
  { name: 'fold-open', width: 884, height: 1104, mobile: true },
  { name: 'ipad-pro-11', width: 834, height: 1194, mobile: true },
  { name: 'ipad-pro-13', width: 1024, height: 1366, mobile: true },
  { name: 'ipad-mini-landscape', width: 1133, height: 744, mobile: true },
  { name: 'ipad-landscape', width: 1080, height: 810, mobile: true },
  { name: 'ipad-air-landscape', width: 1180, height: 820, mobile: true },
  { name: 'ipad-pro-13-landscape', width: 1366, height: 1024, mobile: true },
  { name: 'laptop-hd', width: 1280, height: 720 },
  { name: 'laptop', width: 1366, height: 768 },
  { name: 'macbook-air', width: 1440, height: 900 },
  { name: 'laptop-fhd-scaled', width: 1536, height: 864 },
  { name: 'desktop-fhd', width: 1920, height: 1080 },
  { name: 'desktop-qhd', width: 2560, height: 1440 },
  { name: 'ultrawide', width: 3440, height: 1440 },
  { name: 'short-desktop', width: 1280, height: 600 },
  { name: 'square', width: 1000, height: 1000 },
];

const pages = [
  '/login',
  '/tickets',
  '/tickets/new',
  '/tickets/1048',
  '/tickets/1044',
  '/tickets?announcements=1',
  '/notifications',
  '/account',
  '/admin/tickets',
  '/admin/tickets/1048',
  '/admin/users',
  '/admin/automation',
  '/admin/departments',
  '/admin/announcements',
  '/admin/ai',
];

// Checks run inside the page; each returns human-readable problems.
function inspect(mobile) {
  const problems = [];
  const visible = el => {
    if (!el.getClientRects().length) return false;
    const style = getComputedStyle(el);
    return style.visibility !== 'hidden' && style.opacity !== '0';
  };
  const label = el => {
    const text = (el.getAttribute('aria-label') || el.textContent || el.getAttribute('placeholder') || '').trim().replace(/\s+/g, ' ').slice(0, 40);
    return `${el.tagName.toLowerCase()}${el.className && typeof el.className === 'string' ? '.' + el.className.trim().split(/\s+/).join('.') : ''} "${text}"`;
  };
  const clippedByAncestor = el => {
    for (let p = el.parentElement; p && p !== document.body; p = p.parentElement) {
      const style = getComputedStyle(p);
      if (style.overflowX !== 'visible') return true;
    }
    return false;
  };
  const hidden = el => el.closest('.skip-link, dialog:not([open]), [hidden], .sr-only') || el.closest('details:not([open])') && !el.matches('details:not([open]) > summary, details:not([open]) > summary *');

  if (document.documentElement.scrollWidth > innerWidth) problems.push(`page scrolls horizontally (${document.documentElement.scrollWidth} > ${innerWidth})`);

  for (const el of document.body.querySelectorAll('*')) {
    if (hidden(el) || !visible(el)) continue;
    const rect = el.getBoundingClientRect();
    if (rect.width === 0 || rect.height === 0) continue;
    if ((rect.right > innerWidth + 1 || rect.left < -1) && !clippedByAncestor(el)) problems.push(`outside viewport: ${label(el)} [${Math.round(rect.left)}..${Math.round(rect.right)}]`);
    const style = getComputedStyle(el);
    if ((style.overflowX === 'hidden' || style.overflowX === 'clip') && style.textOverflow !== 'ellipsis' && el.scrollWidth > el.clientWidth + 1 && el.textContent.trim() && !el.matches('.table-scroll, .tabs, [data-scroll]')) {
      problems.push(`content clipped: ${label(el)} (${el.scrollWidth} > ${el.clientWidth})`);
    }
  }

  const interactive = [...document.querySelectorAll('a[href], button, input:not([type=hidden]), select, textarea, summary')].filter(el => !hidden(el) && visible(el));
  for (const el of interactive) {
    const rect = el.getBoundingClientRect();
    const style = getComputedStyle(el);
    if (style.display !== 'inline' && !['checkbox', 'radio', 'file'].includes(el.type) && (rect.width < 24 || rect.height < 24)) problems.push(`small target ${Math.round(rect.width)}x${Math.round(rect.height)}: ${label(el)}`);
    if (mobile && el.matches('input:not([type=checkbox]):not([type=radio]):not([type=file]), select, textarea') && parseFloat(style.fontSize) < 16) problems.push(`input font ${style.fontSize} zooms on iOS focus: ${label(el)}`);
  }
  for (let i = 0; i < interactive.length; i++) {
    for (let j = i + 1; j < interactive.length; j++) {
      const a = interactive[i], b = interactive[j];
      if (a.contains(b) || b.contains(a) || a.closest('label')?.contains(b) || b.closest('label')?.contains(a)) continue;
      const r1 = a.getBoundingClientRect(), r2 = b.getBoundingClientRect();
      const w = Math.min(r1.right, r2.right) - Math.max(r1.left, r2.left);
      const h = Math.min(r1.bottom, r2.bottom) - Math.max(r1.top, r2.top);
      if (w > 2 && h > 2) problems.push(`overlap: ${label(a)} / ${label(b)}`);
    }
  }
  return [...new Set(problems)];
}

test.describe.configure({ mode: 'parallel' });

for (const scheme of ['light', 'dark']) {
  for (const vp of viewports) {
    test(`${scheme} ${vp.name} ${vp.width}x${vp.height}`, async ({ browser }) => {
      const context = await browser.newContext({
        locale: 'zh-TW',
        viewport: { width: vp.width, height: vp.height },
        isMobile: !!vp.mobile,
        hasTouch: !!vp.mobile,
        deviceScaleFactor: vp.mobile ? 2 : 1,
        colorScheme: scheme,
        reducedMotion: 'reduce',
      });
      const page = await context.newPage();
      const errors = [];
      page.on('pageerror', e => errors.push(e.message));
      page.on('console', m => { if (m.type() === 'error') errors.push(m.text()); });
      const report = {};
      for (const path of pages) {
        await page.goto(`http://127.0.0.1:8081${path}`);
        const problems = await page.evaluate(inspect, !!vp.mobile);
        if (errors.length) problems.push(...errors.splice(0).map(e => `console: ${e}`));
        if (problems.length) report[path] = problems;
        if (scheme === 'light' || path === '/login') {
          const dir = `artifacts/viewports/${vp.name}`;
          fs.mkdirSync(dir, { recursive: true });
          await page.screenshot({ path: `${dir}/${scheme}${path.replace(/[/?=]/g, '_')}.png`, fullPage: true });
          // WebKit screenshots inject a style that this site's CSP rightly blocks; only page errors count.
          errors.length = 0;
        }
      }
      await context.close();
      if (Object.keys(report).length) {
        fs.mkdirSync('artifacts/viewport-reports', { recursive: true });
        fs.writeFileSync(`artifacts/viewport-reports/${scheme}-${vp.name}.json`, JSON.stringify(report, null, 2));
      }
      expect(report).toEqual({});
    });
  }
}
