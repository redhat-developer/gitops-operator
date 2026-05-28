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
    await loginPage.loginViaOpenShift();
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
    
    //ignore if missing or rbac locked
    if (response.status() === 404 || response.status() === 403) {
      if (response.status() === 403) console.log('warning: delete forbidden (RBAC) on this cluster; skipping cleanup');
    } else {
      expect(response.status()).toBeLessThan(400);
    }
  }, { timeout: 120000 } ], 
});

//export it so spec files can use it
export { expect };