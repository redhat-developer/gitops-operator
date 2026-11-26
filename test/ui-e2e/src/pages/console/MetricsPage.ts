import { expect, Page } from '@playwright/test';
import { dismissConsoleTour } from './tour';
import { consoleUrl } from './url';

const TIMEOUTS = {
  load: 45000,
};

export class MetricsPage {
  readonly page: Page;

  constructor(page: Page) {
    this.page = page;
  }

  private get runQueriesButton() {
    return this.page
      .locator('[data-test="run-queries-button"]')
      .or(this.page.getByRole('button', { name: /run quer/i }));
  }

  private get queryInput() {
    return this.page.locator('.cm-content').or(this.page.locator('textarea')).first();
  }

  async openAdmin() {
    await this.open('/monitoring/query-browser', /query-browser/);
  }

  async openDeveloper(namespace: string) {
    const ns = encodeURIComponent(namespace);
    await this.open(`/dev-monitoring/ns/${ns}/metrics`, /\/metrics/);
  }

  async runQuery(expression: string) {
    await expect(this.queryInput).toBeVisible({ timeout: TIMEOUTS.load });
    await this.queryInput.click();

    const editor = this.page.locator('.cm-content').first();
    if (await editor.isVisible()) {
      await this.page.keyboard.press('ControlOrMeta+a');
      await this.page.keyboard.insertText(expression);
    } else {
      await this.page.locator('textarea').first().fill(expression);
    }

    await this.runQueriesButton.first().click();
  }

  private async open(path: string, url: RegExp) {
    await this.page.goto(consoleUrl(path), { waitUntil: 'domcontentloaded', timeout: 60000 });
    await dismissConsoleTour(this.page);
    await expect(this.page).toHaveURL(url, { timeout: TIMEOUTS.load });
    //admin uses an h1, developer keeps Metrics as a tab under Observe
    const metricsTitle = this.page
      .getByRole('heading', { name: /^metrics$/i })
      .or(this.page.getByRole('tab', { name: /^metrics$/i }));
    await expect(metricsTitle).toBeVisible({ timeout: TIMEOUTS.load });
    await expect(this.runQueriesButton.first()).toBeVisible({ timeout: TIMEOUTS.load });
  }
}
