import { Page, expect, Locator } from '@playwright/test';

export class ApplicationDetailsPage {
  readonly page: Page;
  readonly resourceTreeContainer: Locator;
  readonly slideOutPanel: Locator;
  readonly logsTab: Locator;

  constructor(page: Page) {
    this.page = page;
    
    //main container
    this.resourceTreeContainer = page.locator('.application-details__tree');
    
    //details panel that slides out (isolate the active visible pane)
    this.slideOutPanel = page.locator('.sliding-panel').filter({ visible: true });

    //logs tab inside the slide-out panel
    this.logsTab = this.slideOutPanel.getByRole('button', { name: /logs/i }).or(this.slideOutPanel.getByText(/logs/i, { exact: true }));
  }

  async verifyResourceTreeLoaded() {
    //wait tree to be visible
    await expect(this.resourceTreeContainer).toBeVisible({ timeout: 20000 });
    
    const appHealthBlock = this.page.locator('div')
      .filter({ has: this.page.getByText('APP HEALTH', { exact: true }) })
      .filter({ hasText: /Healthy/i })
      .last();
      
    await expect(appHealthBlock).toBeVisible({ timeout: 30000 });
  }

  async clickResourceNode(kind: string, name: string) {
    //find the innermost div representing the resource node
    const node = this.resourceTreeContainer
      .locator('div')
      .filter({ hasText: kind })
      .filter({ hasText: name })
      .last();

    //scroll it into view and click it
    await node.scrollIntoViewIfNeeded();
    await node.waitFor({ state: 'visible', timeout: 15000 });
    await node.click();

    //self-healing validation block to handle frontend rendering lag
    await expect(async () => {
      await expect(this.slideOutPanel).toBeVisible({ timeout: 2000 });
    }).toPass({ timeout: 10000 });
  }

  async verifyPodLogs(expectedLogText?: string) {
    //click Logs
    await this.logsTab.waitFor({ state: 'visible', timeout: 5000 });
    await this.logsTab.click();

    const logFilterInput = this.slideOutPanel.getByPlaceholder('containing');
    await expect(logFilterInput).toBeVisible({ timeout: 15000 });

    if (expectedLogText) {
      //find log line anywhere in the slide-out panel
      await expect(this.slideOutPanel).toContainText(expectedLogText, { timeout: 30000 });
    } else {
      const genericLogLine = this.slideOutPanel.getByText(/\d{4}-\d{2}-\d{2}.*(INFO|Started)/).first();
      await expect(genericLogLine).toBeVisible({ timeout: 30000 });
    }
  }
}