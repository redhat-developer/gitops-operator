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
    //re-query on tree re-render
    await expect(async () => {
      const node = this.resourceTreeContainer
        .locator('div')
        .filter({ hasText: kind })
        .filter({ hasText: name })
        .last();
      await expect(node).toBeVisible({ timeout: 5000 });
      await node.click({ timeout: 5000 });
      await expect(this.slideOutPanel).toBeVisible({ timeout: 3000 });
    }).toPass({ timeout: 30000, intervals: [500, 1000, 2000] });
  }

  async verifyPodLogs(expectedLogText?: string) {
    //click Logs timeout 30s 
    await this.logsTab.waitFor({ state: 'visible', timeout: 30000 });
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

  async openAppDetailsPanel() {
    await this.page.getByText('Details', { exact: true }).first().click();
    await expect(this.page.getByText('SYNC POLICY')).toBeVisible({ timeout: 15000 });
  }

  private async confirmArgoPopup() {
    const ok = this.page.locator('[qe-id="argo-popup-ok-button"]');
    await expect(ok).toBeVisible({ timeout: 10000 });
    await ok.click();
    await expect(ok).toBeHidden({ timeout: 15000 });
  }

  private syncPolicyRow(label: RegExp): Locator {
    return this.page
      .locator('.row.white-box__details-row, .white-box__details-row')
      .filter({ hasText: label });
  }

  private async waitForAutomatedFlag(appName: string, flag: 'prune' | 'selfHeal') {
    await expect
      .poll(
        async () => {
          const res = await this.page.request.get(
            `/api/v1/applications/${encodeURIComponent(appName)}`
          );
          if (!res.ok()) return false;
          const app = await res.json();
          return app?.spec?.syncPolicy?.automated?.[flag] === true;
        },
        { timeout: 30000, message: `waiting for automated.${flag}=true on ${appName}` }
      )
      .toBeTruthy();
  }

  //1.19: Enable buttons; 1.20+: checkboxes
  private async enablePruneOrSelfHeal(kind: 'prune' | 'selfHeal') {
    const checkboxId = kind === 'prune' ? 'prune-resources' : 'self-heal';
    const rowLabel = kind === 'prune' ? /PRUNE RESOURCES/i : /SELF HEAL/i;
    const checkbox = this.page.locator(`#${checkboxId}`);
    const enableBtn = this.syncPolicyRow(rowLabel).getByRole('button', { name: /^Enable$/i });

    //wait for checkbox or Enable button
    await expect(checkbox.or(enableBtn).first()).toBeVisible({ timeout: 15000 });
    if (await checkbox.isVisible()) {
      await checkbox.click();
      await this.confirmArgoPopup();
      return;
    }

    await expect(enableBtn).toBeVisible({ timeout: 15000 });
    await enableBtn.click();
    await this.confirmArgoPopup();
  }

  async enableAutoSyncWithPruneAndSelfHeal(appName: string) {
    //1.19: Enable Auto-Sync button; 1.20+: #enable-auto-sync
    const enableBtn = this.page.getByRole('button', { name: /^Enable Auto-Sync$/i });
    const autoSyncCheckbox = this.page.locator('#enable-auto-sync');

    await expect(enableBtn.or(autoSyncCheckbox).first()).toBeVisible({ timeout: 15000 });
    if (await enableBtn.isVisible()) {
      await enableBtn.click();
    } else {
      await autoSyncCheckbox.click();
    }
    await this.confirmArgoPopup();

    await expect(this.page.getByText('AUTOMATED', { exact: true })).toBeVisible({ timeout: 15000 });

    const pruneCheckbox = this.page.locator('#prune-resources');
    const pruneEnableBtn = this.syncPolicyRow(/PRUNE RESOURCES/i).getByRole('button', { name: /^Enable$/i });
    await expect(pruneCheckbox.or(pruneEnableBtn).first()).toBeVisible({ timeout: 15000 });

    await this.enablePruneOrSelfHeal('prune');
    await this.waitForAutomatedFlag(appName, 'prune');

    await this.enablePruneOrSelfHeal('selfHeal');
    await this.waitForAutomatedFlag(appName, 'selfHeal');
    await this.waitForAutomatedFlag(appName, 'prune');
  }

  async assertAutoSyncPruneSelfHealEnabled() {
    await expect(this.page.getByText('AUTOMATED', { exact: true })).toBeVisible();

    const pruneCheckbox = this.page.locator('#prune-resources');
    const selfHealCheckbox = this.page.locator('#self-heal');
    const autoSyncCheckbox = this.page.locator('#enable-auto-sync');

    if (await pruneCheckbox.isVisible() && await selfHealCheckbox.isVisible()) {
      await expect(autoSyncCheckbox).toBeChecked();
      await expect(pruneCheckbox).toBeChecked();
      await expect(selfHealCheckbox).toBeChecked();
      return;
    }

    await expect(
      this.syncPolicyRow(/PRUNE RESOURCES/i).getByRole('button', { name: /^Disable$/i })
    ).toBeVisible();
    await expect(
      this.syncPolicyRow(/SELF HEAL/i).getByRole('button', { name: /^Disable$/i })
    ).toBeVisible();
  }
}
