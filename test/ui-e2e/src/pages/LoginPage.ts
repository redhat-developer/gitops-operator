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

  //login Via OpenShift SSO
  async loginViaOpenShift(user: string, pass: string, idp: string = 'kube:admin') {
    //click the SSO button on the Argo CD landing page
    const ssoButton = this.page.getByText(/LOG IN VIA OPENSHIFT/i);
    await ssoButton.waitFor({ state: 'visible', timeout: 10000 });
    await ssoButton.click();

    //handle the OpenShift IDP selection screen if it appears
    try {
        const idpButton = this.page.getByText(idp, { exact: true });
        await idpButton.waitFor({ state: 'visible', timeout: 3000 });
        await idpButton.click();
    } catch (e) {
        //if it's not there then OpenShift likely defaulted to another
    }

    //fil out the OpenShift credentials 
    await this.page.getByLabel(/Username/i).waitFor({ state: 'visible' });
    await this.page.getByLabel(/Username/i).fill(user);
    await this.page.getByLabel(/Password/i).fill(pass);
    await this.page.getByRole('button', { name: /Log in/i }).click();

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

  //login As Local Admin
  async loginAsLocalAdmin(password: string) {
    //force the local login screen by appending ?dex=none
    await this.page.goto(`${this.page.url()}?dex=none`);
    
    //fill out the local admin credentials
    const userField = this.page.getByRole('textbox').first();
    await userField.waitFor({ state: 'visible' });
    await userField.fill('admin');
    
    await this.page.locator('input[type="password"]').fill(password);
    
    //Click sign in
    await this.page.getByRole('button', { name: /sign in/i }).click();
  }
}