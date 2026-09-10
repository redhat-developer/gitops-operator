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

  async navigate() {
    await this.page.goto('/applications');
    
    //ignore the "failed to load data" banner if it appears
    const errorBanner = this.page.getByText('try again');
    try {
      //wait 3 secs
      await errorBanner.waitFor({ state: 'visible', timeout: TIMEOUTS.short });
      await errorBanner.click(); 
    } catch (error) {
      //ignore if the banner timed out (wasn't present)
      if (error instanceof Error && error.name === 'TimeoutError') {
        //banner didn't appear so just continue
      } else {
        throw error;
      }
    }
    
    await expect(this.newAppButton).toBeVisible({ timeout: TIMEOUTS.default });
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
    
    //handle the "failed to load data" banner if it appears inside the slide-out panel
    const errorBanner = this.page.getByText('try again');
    try {
      await errorBanner.waitFor({ state: 'visible', timeout: TIMEOUTS.short });
      await errorBanner.click(); 
    } catch (error) {
      //ignore if the banner timed out (wasn't present)
      if (error instanceof Error && error.name === 'TimeoutError') {
        // banner didn't appear so just continue
      } else {
        throw error;
      }
    }

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
    //re-apply search filter just in case the UI refreshed
    await this.page.getByPlaceholder(/Search applications/i).fill(appName);
    
    //find the container, then specifically click the link of the app name
    const appLink = this.page.locator('.white-box, .argo-table-list__row')
                         .filter({ has: this.page.getByText(appName, { exact: true }) })
                         .getByRole('link', { name: appName, exact: true });
                             
    await appLink.waitFor({ state: 'visible', timeout: TIMEOUTS.default });
    await appLink.click();
    
    //wait for the URL to change to the details page to ensure the click worked
    await expect(this.page).toHaveURL(/.*\/applications\/.*\/.*/, { timeout: TIMEOUTS.default });
  }
}