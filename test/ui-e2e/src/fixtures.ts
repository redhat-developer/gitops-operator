import { test as base, expect } from '@playwright/test';
import { LoginPage } from './pages/LoginPage';
import { ApplicationsPage } from './pages/ApplicationsPage';

//define custom fixture types
type MyFixtures = {
  managedApp: string;
  argoVersion: string;
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

  //get target argocd version
  argoVersion: async ({ page }, use) => {
      try {
        //get version
        const response = await page.request.get('/api/version');
        
        if (!response.ok()) {
          throw new Error(`API returned status: ${response.status()}`);
        }

        const data = await response.json();
        const fullVersion = data.Version || 'Unknown';
        
        //extract the major.minor version (e.g., "v2.10.1" -> "2.10")
        const match = fullVersion.match(/v(\d+\.\d+)/);
        const version = match ? match[1] : '3.0';
        
        //for debugging/CI logs
        console.log(`TARGETING ARGO CD VERSION: ${fullVersion}`);

        await use(version);
      } catch (error) {
        console.warn(`\n[warn] Failed to fetch Argo CD version from API. Defaulting to 3.0. Reason: ${error instanceof Error ? error.message : 'Unknown'}\n`);
        await use('3.0'); // Default to 3.0
      }
    },

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
    
    //page.request
    const deleteResponse = await page.request.delete(`/api/v1/applications/${appName}?cascade=true`, {
      headers: { 'Content-Type': 'application/json' }
    });
    
    // If it's already 404 (or 403), we have nothing left to do
    if (deleteResponse.status() === 404 || deleteResponse.status() === 403) {
      console.log(`[teardown] ${appName} was already deleted.`);
      return; 
    } else {
      // Ensure the delete request itself was accepted (200/202)
      expect(deleteResponse.status()).toBeLessThan(400);

      console.log(`[teardown] waiting for background cleanup of ${appName} to finish...`);
      await expect.poll(async () => {
        try {
          const checkResponse = await page.request.get(`/api/v1/applications/${appName}`);
          const status = checkResponse.status();
          
          //404 (Not Found) or 403 (Forbidden due to RBAC project scoping)
          return status === 404 || status === 403;
        } catch (error) {
          //router blips or drops the socket swallow it and keep polling
          if (error instanceof Error && (error.message.includes('hang up') || error.message.includes('RESET') || error.message.includes('closed'))) {
            return false; 
          }
          //fail fast
          throw error;
        }
      }, {
        message: `Waiting for ${appName} to completely delete from the cluster.`,
        timeout: 60000, 
        intervals: [2000, 5000, 10000], 
      }).toBeTruthy();
      
      console.log(`[teardown] ${appName} successfully removed from the cluster.`);
    }
  }, { timeout: 300000 } ], 
});

//export it so spec files can use it
export { expect };