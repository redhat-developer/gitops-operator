import { test as setup } from '@playwright/test';

const authFile = '.auth/storageState.json';

setup('authenticate to OpenShift Cluster', async ({ page, baseURL }) => {
  // Navigate to the OpenShift Console
  const targetUrl = baseURL || process.env.CONSOLE_URL || process.env.BASE_URL;

  if (!targetUrl) {
    throw new Error("No Console URL provided! Ensure your bash script exports BASE_URL or CONSOLE_URL.");
  }

  console.log(`Navigating to OpenShift Console: ${targetUrl}`);
  await page.goto(targetUrl); 

  //set locators 
  const idpScreenText = page.getByText(/Log in with/i);
  const usernameInput = page.getByLabel(/Username/i)
    .or(page.locator('input[name="username"]'))
    .or(page.getByPlaceholder(/Username/i));

  //wait for the IDP screen OR the Username field to appear
  try {
    await Promise.race([
      idpScreenText.waitFor({ state: 'visible', timeout: 15000 }),
      usernameInput.waitFor({ state: 'visible', timeout: 15000 })
    ]);
  } catch (e) {
    console.log("Timed out waiting for OpenShift login page to render.");
  }

  const idpName = process.env.IDP || 'kube:admin'; 
  const user = process.env.CLUSTER_USER || 'kubeadmin';

  if (await idpScreenText.isVisible()) {
    console.log(`IDP selection screen detected. Selecting provider: "${idpName}"`);
    
    // look for the specific IDP 
    const idpLink = page.getByRole('link', { name: new RegExp(idpName, 'i') });
    
    await idpLink.waitFor({ state: 'visible', timeout: 5000 });
    await idpLink.click();
  } else {
    console.log("No IDP screen detected (or already selected), proceeding to credentials...");
  }

  // fill in the Credentials
  await usernameInput.waitFor({ state: 'visible', timeout: 10000 });
  await usernameInput.fill(user); 

  const passwordInput = page.getByLabel(/Password/i)
    .or(page.locator('input[name="password"]'))
    .or(page.getByPlaceholder(/Password/i));

  if (!process.env.CLUSTER_PASSWORD) {
      throw new Error("CLUSTER_PASSWORD is not set in the environment!");
  }

  await passwordInput.fill(process.env.CLUSTER_PASSWORD);
  await page.getByRole('button', { name: /Log in/i }).click();

  //save the auth state
  await page.waitForLoadState('networkidle');
  await page.context().storageState({ path: authFile });
});