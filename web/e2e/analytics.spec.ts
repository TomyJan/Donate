import { expect, test } from '@playwright/test';

test('startup analytics configuration loads the external tracker and permits collection under CSP', async ({
  page,
}) => {
  const blocked: string[] = [];
  await page.exposeFunction('recordCSPViolation', (directive: string) => blocked.push(directive));
  await page.addInitScript(() => {
    document.addEventListener('securitypolicyviolation', (event) => {
      (window as Window & { recordCSPViolation: (directive: string) => void }).recordCSPViolation(
        event.effectiveDirective,
      );
    });
  });
  for (const path of ['/', '/privacy', '/admin']) {
    await page.goto(path);
    const tracker = page.locator('script[data-website-id]');
    await expect(tracker).toHaveCount(1);
    await expect(tracker).toHaveAttribute('data-website-id', 'a4dca193-c108-47d7-a429-b89c13c7f747');
    await expect(tracker).toHaveAttribute('data-exclude-search', 'true');
    await expect(tracker).toHaveAttribute('data-exclude-hash', 'true');
    await expect(tracker).toHaveAttribute('defer', '');
    await expect(page.locator('html')).toHaveAttribute('data-umami-collected', 'true');
  }
  expect(blocked).toEqual([]);
});
