import { Page, expect, Locator } from '@playwright/test';

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
      await errorBanner.waitFor({ state: 'visible', timeout: 3000 });
      await errorBanner.click(); 
    } catch (error) {
      //banner didn't appear so just continue
    }
    
    await expect(this.newAppButton).toBeVisible({ timeout: 15000 });
  }

  //helper for fields that need to have select a pre existing option
  async fillDropdown(locator: Locator, value: string) {
    await locator.click();
    await locator.pressSequentially(value, { delay: 50 }); 
    
    //Wait for the dropdown 
    await expect(locator).toHaveValue(value, { timeout: 5000 });
    
    await locator.press('Enter');
  }

  async createApp(appName: string, repoUrl: string, repoPath: string) {
    await this.newAppButton.click();
    await this.page.getByText('Loading...').first().waitFor({ state: 'hidden', timeout: 15000 });

    await this.appNameInput.fill(appName);
    await this.fillDropdown(this.projectInput, 'default'); 
    
    //src
    await this.repoUrlInput.fill(repoUrl);
    await this.pathInput.fill(repoPath);
    
    //dest
    await this.clusterUrlInput.fill('https://kubernetes.default.svc');
    
    //deploy
    await this.namespaceInput.fill('openshift-gitops');
    await this.createButton.click();
  }

async syncApplication(appName: string, expectedResource: string = 'spring-petclinic') {
    //search for app
    await this.page.getByPlaceholder(/Search applications/i).fill(appName);

    const appContainer = this.page.locator('.white-box, .argo-table-list__row').filter({ hasText: appName });
    await appContainer.waitFor({ state: 'visible', timeout: 20000 });
    await appContainer.getByText('Sync', { exact: true }).click();
    
    //slideout panel 
    // Wait for the manifests to fetch from Git and render on the panel
    await expect(this.page.getByText(expectedResource).first()).toBeVisible({ timeout: 15000 });

    //click 'all' to ensure all resource checkboxes are ticked across all Argo CD versions
    const allLink = this.page.getByRole('link', { name: 'all', exact: true });
    try {
      await allLink.waitFor({ state: 'visible', timeout: 3000 });
      await allLink.click();
    } catch (error) {
      //all link didn't appear within 3 sec
    }
    //click the main sync button
    await this.page.getByRole('button', { name: /^synchronize$/i }).first().click();

    //wait for the panel to  close 
    await expect(this.page.getByText('SYNCHRONIZE RESOURCES')).toBeHidden({ timeout: 10000 });
  }

  async verifyStatus(appName: string) {
    //re-apply search filter just in case
    await this.page.getByPlaceholder(/Search applications/i).fill(appName);
    const appContainer = this.page.locator('.white-box, .argo-table-list__row').filter({ hasText: appName });
    
    //90 secs
    await expect(appContainer.getByText(/synced/i)).toBeVisible({ timeout: 90000 });
    await expect(appContainer.getByText(/healthy/i)).toBeVisible({ timeout: 90000 });
  }
}