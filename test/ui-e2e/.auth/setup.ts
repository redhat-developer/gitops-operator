import { test as setup } from '@playwright/test';

const authFile = '.auth/storageState.json';

setup('authenticate to OpenShift Cluster', async ({ page, baseURL }) => {
  // 1. Navigate to the OpenShift Console
  // It checks Playwright's config (baseURL) first, then falls back to environment variables
  const targetUrl = baseURL || process.env.CONSOLE_URL || process.env.BASE_URL;

  if (!targetUrl) {
    throw new Error("No Console URL provided! Ensure your bash script exports BASE_URL or CONSOLE_URL.");
  }

  console.log(`Navigating to OpenShift Console: ${targetUrl}`);
  await page.goto(targetUrl); // <-- THIS WAS THE MISSING LINK!

  // 2. Define our locators flexibly
  const idpScreenText = page.getByText(/Log in with/i);
  const usernameInput = page.getByLabel(/Username/i)
    .or(page.locator('input[name="username"]'))
    .or(page.getByPlaceholder(/Username/i));

  // 3. Wait for EITHER the IDP screen OR the Username field to appear
  try {
    await Promise.race([
      idpScreenText.waitFor({ state: 'visible', timeout: 15000 }),
      usernameInput.waitFor({ state: 'visible', timeout: 15000 })
    ]);
  } catch (e) {
    console.log("Timed out waiting for OpenShift login page to render.");
  }

  // Set a default user to prevent undefined errors if you forget to export it
  const user = process.env.CLUSTER_USER || 'kubeadmin';

  // 4. Handle the IDP Screen if it exists
  if (await idpScreenText.isVisible()) {
    console.log("IDP selection screen detected. Selecting provider...");
    
    // Decide which IDP to click based on the user
    const idpRegex = (user === 'kubeadmin') ? /kube:admin/i : /htpasswd/i;
    
    // OpenShift IDPs are usually links styled as buttons
    await page.getByRole('link', { name: idpRegex }).click();
  } else {
    console.log("No IDP screen detected, proceeding directly to credentials...");
  }

  // 5. Fill in Cluster Credentials
  await usernameInput.waitFor({ state: 'visible', timeout: 10000 });
  await usernameInput.fill(user); // Using the fallback variable defined above

  const passwordInput = page.getByLabel(/Password/i)
    .or(page.locator('input[name="password"]'))
    .or(page.getByPlaceholder(/Password/i));

  // Assert that password exists so we don't accidentally type 'undefined'
  if (!process.env.CLUSTER_PASSWORD) {
      throw new Error("CLUSTER_PASSWORD is not set in the environment!");
  }

  await passwordInput.fill(process.env.CLUSTER_PASSWORD);
  await page.getByRole('button', { name: /Log in/i }).click();

  // 6. Save this pure OpenShift auth state
  // Added a brief wait to ensure login completes before saving state
  await page.waitForLoadState('networkidle');
  await page.context().storageState({ path: authFile });
});