import { test, expect } from '@playwright/test';
import { execSync } from 'node:child_process';

test('Log into Argo CD as local admin', async ({ browser }) => {
  const rawOutput = execSync(
    'oc extract secret/openshift-gitops-cluster -n openshift-gitops --keys=admin.password --to=-'
  ).toString();
  
  //get credentials
  const password = rawOutput.split('\n').map(l => l.trim()).filter(l => l && !l.startsWith('#'))[0];

  const routeUrl = execSync(
    'oc get route openshift-gitops-server -n openshift-gitops -o jsonpath="{.spec.host}"'
  ).toString().trim();

  //Fresh context to avoid any cached state issues
  const context = await browser.newContext({ 
    storageState: { cookies: [], origins: [] },
    ignoreHTTPSErrors: true 
  });
  
  //Navigate and wait for the page to be loaded
  const page = await context.newPage();
  const loginUrl = `https://${routeUrl}/login?dex=none`;
  await page.goto(loginUrl, { waitUntil: 'load' });

  const userField = page.getByRole('textbox').first();
  await userField.waitFor({ state: 'visible', timeout: 20000 });

  //Fill and Sign In
  await userField.fill('admin');
  await page.locator('input[type="password"]').fill(password);
  await page.getByRole('button', { name: /sign in/i }).click();

  //Verify we're logged in
  await expect(page.locator('.sidebar, [data-testid="sidebar"]').first()).toBeVisible({ timeout: 20000 });

  await context.close();
});