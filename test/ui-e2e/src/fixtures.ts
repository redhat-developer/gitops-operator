import { test as base, expect } from '@playwright/test';
import { LoginPage } from './pages/LoginPage';
import { ApplicationsPage } from './pages/ApplicationsPage';

//define custom fixture types
type MyFixtures = {
  managedApp: string;
};

export const test = base.extend<MyFixtures>({
  
  //login override
  page: async ({ page }, use) => {
    const loginPage = new LoginPage(page);
    await loginPage.goto();
    
    // 1. Grab variables from the environment
    const user = process.env.CLUSTER_USER || 'kubeadmin';
    const pass = process.env.CLUSTER_PASSWORD;
    const idp = process.env.IDP || 'kube:admin';

    // 2. Fail loudly if the password is missing
    if (!pass) {
      throw new Error('CLUSTER_PASSWORD environment variable is missing. Cannot authenticate.');
    }

    // 3. Pass them into the login method
    await loginPage.loginViaOpenShift(user, pass, idp);
    
    await use(page);
  },

//app setup/teardown
  managedApp: [ async ({ page }, use) => {
    const appName = `e2e-app-${Date.now()}`;
    const appsPage = new ApplicationsPage(page);
    
    console.log(`[setup] creating and syncing application: ${appName}`);
    await appsPage.navigate();
    await appsPage.createApp(
      appName, 
      'https://github.com/redhat-developer/openshift-gitops-getting-started.git', 
      'app'
    );
    await appsPage.syncApplication(appName);
    await appsPage.verifyStatus(appName);

    //pass the name to the test
    await use(appName);

    //teardown 
    console.log(`[teardown] deleting ${appName} via api`);
    const response = await page.request.delete(`/api/v1/applications/${appName}?cascade=true`, {
      headers: { 'Content-Type': 'application/json' }
    });
    
    // 4. Update the teardown to only ignore 404s, treating 403s as failures
    if (response.status() === 404) {
      return; 
    } else {
      expect(response.status()).toBeLessThan(400);
    }
  }, { timeout: 120000 } ], 
});

//export it so spec files can use it
export { expect };