import { expect, Page } from '@playwright/test';

//tour popup eats the first click
export async function dismissConsoleTour(page: Page) {
  const dialog = page.getByRole('dialog').filter({
    has: page.getByRole('heading', { name: /welcome to the new openshift experience/i }),
  });

  try {
    await dialog.waitFor({ state: 'visible', timeout: 2000 });
  } catch (error) {
    if (error instanceof Error && error.name === 'TimeoutError') return;
    throw error;
  }

  await dialog.getByRole('button', { name: /skip tour|^close$/i }).first().click();
  await expect(dialog).toBeHidden();
}
