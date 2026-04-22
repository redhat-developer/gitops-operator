import { test, expect } from '@playwright/test';
import { LoginPage } from '../src/pages/LoginPage';

test.describe('Authentication Flow', () => {
  
  // don't not use the saved login state
  // make sure we always get to the argo login screen, even after setup.ts already ran.
  test.use({ storageState: { cookies: [], origins: [] } });

  //login via openshift
  test('Scenario: Successful OpenShift SSO Login', async ({ page }) => {
    const loginPage = new LoginPage(page);
    await loginPage.goto();

    await loginPage.loginViaOpenShift(
      process.env.CLUSTER_USER!,
      process.env.CLUSTER_PASSWORD!,
      process.env.IDP
    );

    const newAppButton = page.getByRole('button', { name: /NEW APP/i });
    await expect(newAppButton).toBeVisible({ timeout: 15000 });
  });
});