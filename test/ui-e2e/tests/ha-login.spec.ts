import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import { LoginPage } from '../src/pages/LoginPage';
import { enableHA, disableHA } from '../src/utils/ha-manager';

test.describe.configure({ mode: 'serial' });

test.describe('HA Login Verification', () => {

  test.use({ storageState: { cookies: [], origins: [] } });

  test.beforeAll(async ({}, testInfo) => {
    testInfo.setTimeout(600000); //10 mins for ha rollout
    try {
      await enableHA();
    } catch (error) {
      console.error('[setup] Failed to enable HA. Aborting tests.', error);
      throw error;
    }
  });

  test.afterAll(async ({}, testInfo) => {
    testInfo.setTimeout(300000); //5 mins for teardown
    try {
      await disableHA();
    } catch (error) {
      console.error('[teardown] Failed to disable HA. Cluster may be in a dirty state.', error);
      throw error;
    }
  });

  test('Local Admin Login under HA', async ({ page }) => {
    test.setTimeout(120000);

    let rawOutput = execSync('oc extract secret/openshift-gitops-cluster -n openshift-gitops --keys=admin.password --to=-', { timeout: 30000 }).toString();
    const adminPassword = rawOutput.split('\n').map(l => l.trim()).filter(l => l && !l.startsWith('#'))[0];

    if (!adminPassword) {
      throw new Error('failed to extract admin password from cluster secret');
    }

    await page.goto('/login?dex=none', { waitUntil: 'load' });
    const userField = page.getByLabel(/username/i);
    await userField.waitFor({ state: 'visible', timeout: 30000 });

    await userField.fill('admin');
    await page.locator('input[type="password"]').fill(adminPassword);
    await page.getByRole('button', { name: /sign in/i }).click();

    await expect(page.getByText('Applications', { exact: true }).first()).toBeVisible({ timeout: 30000 });
  });

  test('OpenShift SSO Login under HA', async ({ page }) => {
    test.setTimeout(120000);

    const loginPage = new LoginPage(page);
    await loginPage.goto();

    await loginPage.loginViaOpenShift(
      process.env.CLUSTER_USER!,
      process.env.CLUSTER_PASSWORD!,
      process.env.IDP || 'kube:admin'
    );

    await expect(page.getByText('Applications', { exact: true }).first()).toBeVisible({ timeout: 30000 });
  });

});