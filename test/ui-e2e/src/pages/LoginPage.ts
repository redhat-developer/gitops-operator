import { Page } from '@playwright/test';

export class LoginPage {
  readonly page: Page;

  constructor(page: Page) {
    this.page = page;
  }

  async goto() {
    //navigate to the baseURL defined in playwright.config.ts
    await this.page.goto('/');
  }

  async loginViaOpenShift(user?: string, pass?: string, idp: string = 'kube:admin') {
    const ssoButton = this.page.getByText(/LOG IN VIA OPENSHIFT/i);
    const newAppButton = this.page.getByRole('button', { name: /NEW APP/i });

    //wait dynamically for either the login screen OR the dashboard to render
    await ssoButton.or(newAppButton).first().waitFor({ state: 'visible', timeout: 20000 });

    //if we landed straight on the dashboard, the cluster was already fully authenticated
    if (await newAppButton.isVisible()) {
      return;
    }

    //otherwise, click the SSO button on the Argo CD landing page
    await ssoButton.click();

    //handle the OpenShift IDP selection screen if it appears
    try {
        const idpButton = this.page.getByText(idp, { exact: true });
        await idpButton.waitFor({ state: 'visible', timeout: 3000 });
        await idpButton.click();
    } catch (e) {
        //if it's not there then OpenShift likely defaulted to another
    }

    //check if manual login is actually required
    const usernameInput = this.page.getByRole('textbox', { name: /Username/i })
                                   .or(this.page.locator('input[name="username"]'))
                                   .or(this.page.getByPlaceholder(/Username/i))
                                   .first();
                                   
    const needsLogin = await usernameInput.waitFor({ state: 'visible', timeout: 5000 }).then(() => true).catch(() => false);

    if (needsLogin && user && pass) {
        //fill out the OpenShift credentials
        await usernameInput.fill(user);
        await this.page.getByLabel(/Password/i).fill(pass);
        await this.page.getByRole('button', { name: /Log in/i }).click();
      } else if (needsLogin) {
        throw new Error('Login required but credentials (user/pass) not provided');
      }

    //Auth Handle the Allow Permissions screen
    try {
      const allowButton = this.page.getByRole('button', { name: /Allow selected permissions/i });
      await allowButton.waitFor({ state: 'visible', timeout: 5000 });
      await allowButton.click();
    } catch (error) {
      // Screen didn't appear likely already authorised. 
    }

    //Success Checking make we land on the applications dashboard
    await this.page.waitForURL('**/applications**', { timeout: 20000 }); 
  }
}