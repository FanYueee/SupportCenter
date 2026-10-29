const { test, expect } = require('@playwright/test');
const fs = require('node:fs');

test.describe('English layout', () => {
  test.use({ locale: 'en-US' });

  for (const width of [320, 390, 768, 1024, 1280, 1440]) {
    test(`${width}px: translated labels remain separate`, async ({ page }) => {
      await page.setViewportSize({ width, height: 900 });
      const paths = ['/tickets', '/tickets/new', '/tickets/1048', '/tickets/1044', '/notifications', '/admin/tickets', '/admin/tickets/1048', '/admin/automation', '/admin/ai', '/admin/departments', '/admin/account'];
      for (const path of paths) {
        await page.goto(path);
        await expect(page.locator('html')).toHaveAttribute('lang', 'en');
        const problems = await page.evaluate(() => {
          const errors = [];
          const box = element => element.getBoundingClientRect();
          const visible = element => element.getClientRects().length > 0;
          const textBounds = element => {
            const range = document.createRange();
            range.selectNodeContents(element);
            return range.getBoundingClientRect();
          };
          if (document.documentElement.scrollWidth > innerWidth) errors.push('Page overflows the viewport');

          const brand = box(document.querySelector('.brand'));
          const actions = box(document.querySelector('.header-actions'));
          if (brand.right > actions.left + 1) errors.push('Brand overlaps account actions');

          for (const item of document.querySelectorAll('.nav-item, .tab, .phase-tab, .priority-tab, .settings-tab')) {
            if (item.scrollWidth > item.clientWidth + 1) errors.push(`Tab is squeezed: ${item.textContent}`);
          }
          for (const item of document.querySelectorAll('.main-nav .nav-item, .tabs .tab')) {
            const bounds = textBounds(item);
            const container = box(item.parentElement);
            if (bounds.left < container.left - 1 || bounds.right > container.right + 1) errors.push(`Navigation text is clipped: ${item.textContent}`);
          }
          for (const input of document.querySelectorAll('.search input[placeholder]')) {
            const style = getComputedStyle(input);
            const context = document.createElement('canvas').getContext('2d');
            context.font = style.font;
            const available = input.clientWidth - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight);
            if (context.measureText(input.placeholder).width > available + 1) errors.push('Search hint is clipped');
          }
          for (const button of document.querySelectorAll('.locale-button')) {
            for (const child of button.children) {
              if (box(child).right > box(button).right + 1) errors.push('Language label or checkmark escapes its button');
            }
          }

          for (const group of document.querySelectorAll('.ticket-person-group')) {
            const label = textBounds(group.querySelector('.ticket-person-label'));
            const person = box(group.children[1]);
            if (label.right + 6 > person.left) errors.push('Staff label overlaps the person');
          }

          for (const badge of document.querySelectorAll('.table-scroll .badge')) {
            if (!visible(badge)) continue;
            const cell = box(badge.closest('td'));
            const bounds = box(badge);
            if (bounds.left < cell.left - 1 || bounds.right > cell.right + 1) errors.push(`Badge extends outside its cell: ${badge.textContent}`);
          }

          for (const term of document.querySelectorAll('.metadata-panel dt')) {
            const value = term.nextElementSibling;
            if (box(term).right + 10 > box(value).left) errors.push('Ticket metadata columns overlap');
            const range = document.createRange();
            range.selectNodeContents(value);
            for (const bounds of range.getClientRects()) {
              if (bounds.right > box(value).right + 1 || bounds.left < box(value).left - 1) errors.push(`Metadata text escapes its column: ${value.textContent}`);
            }
          }

          for (const header of document.querySelectorAll('.message-header')) {
            const author = box(header.querySelector('strong').parentElement);
            const time = box(header.querySelector('.message-time'));
            const sameRow = author.top < time.bottom && time.top < author.bottom;
            if (sameRow && author.right + 6 > time.left) errors.push('Author overlaps the timestamp');
          }

          for (const summary of document.querySelectorAll('.day-schedule summary')) {
            const day = box(summary.querySelector('strong'));
            const hours = box(summary.querySelector('span'));
            if (day.right + 10 > hours.left) errors.push('Day overlaps service hours');
          }
          return errors;
        });
        expect(problems, `${path} at ${width}px`).toEqual([]);

        if (path === '/admin/tickets' || path === '/admin/tickets/1048') {
          fs.mkdirSync('artifacts', { recursive: true });
          await page.screenshot({ path: `artifacts/english-${path.endsWith('1048') ? 'detail' : 'workspace'}-${width}.png`, fullPage: true });
        }
      }
    });
  }
});
