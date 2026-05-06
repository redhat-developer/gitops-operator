import { test, expect } from '@playwright/test';
import { LoginPage } from '../src/pages/LoginPage';

test.describe('Argo CD SSO Authentication', () => {
  
  //clear storageState to force a full login flow for this specific test
  test.use({ storageState: { cookies: [], origins: [] } });

  test('should successfully log in via OpenShift SSO', async ({ page }) => {
    const loginPage = new LoginPage(page);
    
    await loginPage.goto();

    await loginPage.loginViaOpenShift(
      process.env.CLUSTER_USER!,
      process.env.CLUSTER_PASSWORD!,
      process.env.IDP || 'kube:admin'
    );

    //Check the button is visible as proof of successful login
    await expect(page.getByRole('button', { name: /NEW APP/i })).toBeVisible({ timeout: 15000 });
  });
});