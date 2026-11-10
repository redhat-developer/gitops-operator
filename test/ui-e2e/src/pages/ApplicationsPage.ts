import { Page, expect, Locator } from '@playwright/test';

//timeouts
const TIMEOUTS = {
  short: 3000,
  modal: 5000,
  panel: 10000,
  default: 15000,
  load: 20000,
  render: 30000,
  sync: 120000,
  status: 180000
};

export class ApplicationsPage {
  readonly page: Page;
  readonly newAppButton: Locator;
  readonly appNameInput: Locator;
  readonly projectInput: Locator;
  readonly repoUrlInput: Locator;
  readonly pathInput: Locator;
  readonly clusterUrlInput: Locator;
  readonly namespaceInput: Locator;
  readonly createButton: Locator;

  constructor(page: Page) {
    this.page = page;
    
    //header buttons
    this.newAppButton = page.getByRole('button', { name: /NEW APP/i });
    this.createButton = page.getByRole('button', { name: 'Create', exact: true });    

    this.appNameInput = page.getByLabel('Application Name', { exact: true });
    this.projectInput = page.locator('[qe-id="application-create-field-project"]');
    
    //src
    this.repoUrlInput = page.locator('[qe-id="application-create-field-repository-url"]')
                            .or(page.getByPlaceholder(/github\.com/i)).first();
    
    this.pathInput = page.locator('[qe-id="application-create-field-path"]')
                         .or(page.getByText('Path').locator('..').locator('input')).first();
    //dest
    this.clusterUrlInput = page.locator('[qe-id="application-create-field-cluster-url"]')
                               .or(page.getByText('Cluster URL', { exact: true }).locator('..').locator('input')).first();
    
    this.namespaceInput = page.locator('[qe-id="application-create-field-namespace"]')
                              .or(page.getByText('Namespace', { exact: true }).locator('..').locator('input')).first();
                              
  }

  //dismiss intermittent load-error banner (wait only for late banners)
  private async dismissLoadErrorBanner(waitForLateMs = 0) {
    const errorBanner = this.page.getByText(/try again/i);
    if (await errorBanner.isVisible()) {
      await errorBanner.click();
      return;
    }
    if (waitForLateMs <= 0) return;
    try {
      await errorBanner.waitFor({ state: 'visible', timeout: waitForLateMs });
    } catch (error) {
      if (error instanceof Error && error.name === 'TimeoutError') return;
      throw error;
    }
    await errorBanner.click();
  }

  async navigate() {
    const attempts = 3;
    let lastError: unknown;

    for (let attempt = 1; attempt <= attempts; attempt++) {
      await this.page.goto('/applications', { waitUntil: 'domcontentloaded' });
      await this.dismissLoadErrorBanner();

      try {
        await expect(this.newAppButton).toBeVisible({ timeout: TIMEOUTS.render });
        return;
      } catch (error) {
        lastError = error;
        //dismiss late load-error banner
        await this.dismissLoadErrorBanner(TIMEOUTS.modal);
        if (attempt < attempts) {
          console.log(
            `[apps] NEW APP not ready on /applications (attempt ${attempt}/${attempts}); reloading...`
          );
        }
      }
    }

    throw lastError;
  }

  //helper for fields that need to have select a pre existing option
  async fillDropdown(locator: Locator, value: string) {
    await locator.click();
    await locator.pressSequentially(value, { delay: 50 }); 
    
    //wait for the dropdown 
    await expect(locator).toHaveValue(value, { timeout: TIMEOUTS.modal });
    
    await locator.press('Enter');
  }

  async createApp(appName: string, repoUrl: string, repoPath: string) {
    await this.newAppButton.click();
    await this.dismissLoadErrorBanner();

    await this.page.getByText('Loading...').first().waitFor({ state: 'hidden', timeout: TIMEOUTS.default });

    await this.appNameInput.fill(appName);
    await this.fillDropdown(this.projectInput, 'default'); 
    
    //src
    await this.repoUrlInput.fill(repoUrl);
    await this.pathInput.fill(repoPath);
    
    //dest
    await this.clusterUrlInput.fill('https://kubernetes.default.svc');
    
    //deploy to namespace
    await this.namespaceInput.fill('openshift-gitops'); 

    await this.createButton.click();
  }

  async syncApplication(appName: string, expectedResource: string = 'spring-petclinic') {
    //search for app
    await this.page.getByPlaceholder(/Search applications/i).fill(appName);

    const appContainer = this.page.locator('.white-box, .argo-table-list__row').filter({ hasText: appName });
    await appContainer.waitFor({ state: 'visible', timeout: TIMEOUTS.load });
    
    await expect(appContainer.getByText(/OutOfSync|Out of Sync/i).first()).toBeVisible({ timeout: TIMEOUTS.sync });

    //safe to open the panel now
    await appContainer.getByText('Sync', { exact: true }).click();

    const slideOutPanel = this.page.locator('.sliding-panel').filter({ visible: true });
    
    //slideOutPanel
    const allLink = slideOutPanel.getByRole('link', { name: 'all', exact: true });
    try {
      await allLink.waitFor({ state: 'visible', timeout: TIMEOUTS.modal });
      await allLink.click();
    } catch (error) {
      //ignore if the link timed out (absent in older versions)
      if (error instanceof Error && error.name === 'TimeoutError') {
        //'all' link didn't appear which is normal for this version so do nothing.
      } else {
        throw error;
      }
    }

    await expect(slideOutPanel.getByText(expectedResource).first()).toBeVisible({ timeout: TIMEOUTS.render });

    await slideOutPanel.getByRole('button', { name: /^synchronize$/i }).first().click();

    //wait for the panel to close 
    await expect(this.page.getByText('SYNCHRONIZE RESOURCES')).toBeHidden({ timeout: TIMEOUTS.panel });
  }

  async verifyStatus(appName: string) {
    await this.page.getByPlaceholder(/Search applications/i).fill(appName);
    const appContainer = this.page.locator('.white-box, .argo-table-list__row').filter({ hasText: appName });
    
    //pass the message
    await expect(
      appContainer.getByText(/Sync failed/i), 
      `Argo CD failed to sync the application manifests for ${appName}.`
    ).toBeHidden({ timeout: TIMEOUTS.panel });

    //if it didn't fail to wait for success states
    await expect(appContainer.getByText(/synced/i)).toBeVisible({ timeout: TIMEOUTS.status });
    await expect(appContainer.getByText(/healthy/i)).toBeVisible({ timeout: TIMEOUTS.status });
  }

  async openApplication(appName: string) {
    await this.page.getByPlaceholder(/Search applications/i).fill(appName);

    const appCard = this.page
      .locator('.white-box, .argo-table-list__row, .application-tile, [class*="application-tile"], [class*="applications-list__entry"]')
      .filter({ hasText: appName });
    const appNameLink = appCard.getByText(appName, { exact: true }).filter({ visible: true }).first();

    await expect(appNameLink).toBeVisible({ timeout: TIMEOUTS.load });
    await appNameLink.click();
    await expect(this.page).toHaveURL(/.*\/applications\/.*\/.*/, { timeout: TIMEOUTS.default });
  }

  private appTiles(appName: string): Locator {
    return this.page
      .locator(
        '.white-box, .argo-table-list__row, .application-tile, [class*="application-tile"], [class*="applications-list__entry"]'
      )
      .filter({ hasText: appName });
  }

  private appCard(appName: string): Locator {
    return this.appTiles(appName).first();
  }

  private filterGroup(label: string): Locator {
    return this.page.locator('.filter').filter({ has: this.page.getByText(label, { exact: true }) });
  }

  async searchApplications(query: string) {
    await this.page.getByPlaceholder(/Search applications/i).fill(query);
  }

  async clearSearch() {
    const search = this.page.getByPlaceholder(/Search applications/i);
    await search.fill('');
    const clear = this.page.locator('.applications-list__search .fa-times');
    if (await clear.isVisible()) {
      await clear.click();
    }
  }

  async ensureFiltersVisible() {
    const appFilters = this.page.getByText('Application filters', { exact: true });
    if (await appFilters.isVisible()) return;
    //details page also has health status (not treating that as the list)
    if (/\/applications\/[^/?]+\/[^/?]+/.test(this.page.url())) {
      await this.navigate();
    }
    if (await appFilters.isVisible()) return;
    await this.page.locator('.sidebar__collapse-button').click();
    await expect(appFilters).toBeVisible({ timeout: TIMEOUTS.panel });
  }

  async clearAllFilters() {
    await this.ensureFiltersVisible();
    //argo 3.3 names it as "CLEAR" but 3.4+ is "CLEAR ALL"
    const btn = this.page.getByRole('button', { name: /CLEAR(\s+ALL)?/i }).first();
    if (await btn.isVisible()) {
      await btn.click();
      await expect(btn).toBeHidden({ timeout: TIMEOUTS.panel });
    }
  }

  async selectFilterOption(groupLabel: string, option: string) {
    await this.ensureFiltersVisible();
    const group = this.filterGroup(groupLabel);
    const item = group.locator('.filter__item').filter({
      has: this.page.locator('.filter__item__label', { hasText: new RegExp(`^${option}$`) }),
    });
    await expect(item).toBeVisible({ timeout: TIMEOUTS.default });
    const checkbox = item.getByRole('checkbox');
    if ((await checkbox.count()) > 0) {
      await checkbox.click();
      return;
    }
    await item.click();
  }

  async filterByLabel(selector: string) {
    await this.ensureFiltersVisible();
    const group = this.filterGroup('LABELS');
    const input = group.locator('.autocomplete__input');
    if (!(await input.isVisible())) {
      await group.locator('.filter__header__label').click();
    }
    await expect(input).toBeVisible({ timeout: TIMEOUTS.default });
    await input.click();
    await input.fill(selector);
    //argo ui v2 sends items to document.body as autocomplete__items__item
    const suggestion = this.page.locator('.autocomplete__items__item').filter({
      hasText: new RegExp(`^${selector}$`),
    });
    await expect(suggestion).toBeVisible({ timeout: TIMEOUTS.default });
    await suggestion.click();
    await expect(
      group.locator('.filter__item__label', { hasText: new RegExp(`^${selector}$`) })
    ).toBeVisible({ timeout: TIMEOUTS.default });
  }

  async assertAppVisible(appName: string) {
    await expect(this.appTiles(appName).first()).toBeVisible({ timeout: TIMEOUTS.load });
  }

  async assertAppHidden(appName: string) {
    await expect(this.appTiles(appName)).toHaveCount(0, { timeout: TIMEOUTS.load });
  }

  private syncPanel(): Locator {
    return this.page.locator('.sliding-panel').filter({ visible: true }).first();
  }

  //v1.20–v1.22 (or argocd v3.3–v3.5) the heading and card sync control a bit
  async openSyncPanel(appName: string) {
    await this.page.getByPlaceholder(/Search applications/i).fill(appName);
    const card = this.appCard(appName);
    await card.waitFor({ state: 'visible', timeout: TIMEOUTS.load });
    await expect(card.getByText(/OutOfSync|Out of Sync/i).first()).toBeVisible({
      timeout: TIMEOUTS.sync,
    });

    const syncBtn = card
      .getByRole('button', { name: /^sync$/i })
      .or(card.getByText('Sync', { exact: true }));
    await syncBtn.first().click();

    const heading = this.page.getByText(/SYNCHRONIZE RESOURCES/i);
    await expect(this.syncPanel().or(heading).first()).toBeVisible({ timeout: TIMEOUTS.panel });
  }

  async dryRunSynchronize(appName: string) {
    await this.openSyncPanel(appName);
    const panel = this.syncPanel();

    const dryRun = panel
      .getByRole('checkbox', { name: /dry run/i })
      .or(panel.getByLabel(/dry run/i))
      .or(this.page.getByRole('checkbox', { name: /dry run/i }))
      .or(this.page.locator('#dry-run, #dryRun, [qe-id*="dry-run" i]'))
      .first();
    await expect(dryRun).toBeVisible({ timeout: TIMEOUTS.default });
    const alreadyOn = await dryRun.isChecked().catch(() => false);
    if (!alreadyOn) {
      await dryRun.click();
    }

    //need a new op, not the last dry-run still on the app
    const previousOp = await this.applicationOperation(appName);
    const previousId = operationId(previousOp);
    await panel.getByRole('button', { name: /^synchronize$/i }).first().click();

    await expect
      .poll(
        async () => {
          const op = await this.applicationOperation(appName);
          const dry = op?.operation?.sync?.dryRun === true && operationId(op) !== previousId;
          return dry ? String(op?.phase || '') : '';
        },
        { timeout: TIMEOUTS.sync, message: 'waiting for dry-run operation to succeed' }
      )
      .toMatch(/Succeeded/i);
  }

  private async applicationOperation(appName: string) {
    const res = await this.page.request.get(
      `/api/v1/applications/${encodeURIComponent(appName)}`
    );
    if (!res.ok()) return undefined;
    const app = await res.json();
    return app?.status?.operationState;
  }
}

function operationId(op: { startedAt?: string; finishedAt?: string } | undefined) {
  return `${op?.startedAt ?? ''}|${op?.finishedAt ?? ''}`;
}