import { test, expect } from '@playwright/test';
import { execSync } from 'node:child_process';

test('Log into Argo CD as local admin', async ({ browser }) => {
  let rawOutput: string;
  let routeUrl: string;

  try {
    rawOutput = execSync(
      'oc extract secret/openshift-gitops-cluster -n openshift-gitops --keys=admin.password --to=-',
      { timeout: 15000, stdio: 'pipe' }
    ).toString();
  } catch (error) {
    throw new Error("Failed to extract admin password. Please check your cluster connection and oc CLI.", { cause: error });
  }
  
  //get credentials
  const password = rawOutput.split('\n').map(l => l.trim()).filter(l => l && !l.startsWith('#'))[0];

  if (!password || password.length < 8) {
    throw new Error("Extracted password appears invalid. Please verify the secret format in the OpenShift cluster.");
  }
  
  try {
    routeUrl = execSync(
      'oc get route openshift-gitops-server -n openshift-gitops -o jsonpath="{.spec.host}"',
      { timeout: 15000, stdio: 'pipe' }
    ).toString().trim();
  } catch (error) {
    throw new Error("Failed to fetch Argo CD route. Please check your cluster connection and oc CLI.", { cause: error });
  }

  //Fresh context to avoid any cached state issues
  const context = await browser.newContext({ 
    storageState: { cookies: [], origins: [] },
    ignoreHTTPSErrors: true 
  });
  
  try {
      //Navigate and wait for the page to be loaded
      const page = await context.newPage();
      const loginUrl = `https://${routeUrl}/login?dex=none`;
      await page.goto(loginUrl, { waitUntil: 'load' });

      const userField = page.getByLabel(/username/i);
      await userField.waitFor({ state: 'visible', timeout: 20000 });

      //Fill and Sign In
      await userField.fill('admin');
      await page.locator('input[type="password"]').fill(password);
      await page.getByRole('button', { name: /sign in/i }).click();

      //Verify we're logged in
      await expect(page.locator('.sidebar, [data-testid="sidebar"]').first()).toBeVisible({ timeout: 20000 });
    } finally {
      // This guarantees the context closes even if an assertion fails above!
      await context.close();
    }
  });