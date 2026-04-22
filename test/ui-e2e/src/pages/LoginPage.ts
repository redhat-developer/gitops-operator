import { Page, expect } from '@playwright/test';

export class LoginPage {
  readonly page: Page;

  constructor(page: Page) {
    this.page = page;
  }

  async goto() {
    await this.page.goto('/');
  }

  async loginViaOpenShift(user: string, pass: string, idp: string = 'kube:admin') {

    // ======================================================
    //  Argo CD Login Screen
    // ======================================================
    // We just wait patiently. Even if Argo CD does its weird 
    // redirect dance for 2 seconds, Playwright will wait up to 
    // 10 seconds for this button to finally appear.
    const ssoButton = this.page.getByText(/LOG IN VIA OPENSHIFT/i);
    await ssoButton.waitFor({ state: 'visible', timeout: 10000 });
    await ssoButton.click();

    // ======================================================
    // OpenShift Login (with optional IDP step)
    // ======================================================
    // Sometimes OpenShift asks which IDP to use. We check for it quickly.
    try {
        const idpButton = this.page.getByText(idp, { exact: true });
        // Only wait 3 seconds. If it's not there, OpenShift skipped straight to the form.
        await idpButton.waitFor({ state: 'visible', timeout: 3000 });
        await idpButton.click();
        console.log(`Clicked IDP: ${idp}`);
    } catch (e) {
        console.log('No IDP selection screen, proceeding to credentials form...');
    }

    // Now fill out the actual Username/Password form
    await this.page.getByLabel(/Username/i).waitFor({ state: 'visible' });
    await this.page.getByLabel(/Username/i).fill(user);
    await this.page.getByLabel(/Password/i).fill(pass);
    await this.page.getByRole('button', { name: /Log in/i }).click();

    // ======================================================
    // Authorize Access (First Login Only)
    // ======================================================
    try {
      const allowButton = this.page.getByRole('button', { name: 'Allow selected permissions' });
      // Wait 5 seconds. If it's not there, we've already authorized in the past.
      await allowButton.waitFor({ state: 'visible', timeout: 5000 });
      await allowButton.click();
      console.log('Clicked Authorize Access button.');
    } catch (error) {
      console.log('No Authorize screen appeared, continuing...');
    }

    // ======================================================
    // Argo CD Dashboard (Success)
    // ======================================================
    // Wait for the URL to change to the applications dashboard
    await this.page.waitForURL('**/applications**', { timeout: 20000 }); 
  }
}