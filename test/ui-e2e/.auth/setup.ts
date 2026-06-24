import { test as setup, expect } from '@playwright/test';

const authFile = '.auth/storageState.json';

//centralized timeouts to appease the linter
const TIMEOUTS = { short: 5000, medium: 10000, default: 15000, long: 20000 };

setup('authenticate to OpenShift Cluster', async ({ page, baseURL }) => {
  //navigate to the OpenShift Console
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

  //fail loudly if the page is dead so we don't get weird errors later
  await expect(
    idpScreenText.or(usernameInput).first(), 
    "OpenShift login page failed to load. Check cluster health and URL."
  ).toBeVisible({ timeout: TIMEOUTS.long });

  const idpName = process.env.IDP || 'kube:admin'; 
  const user = process.env.CLUSTER_USER || 'kubeadmin';

  if (await idpScreenText.isVisible()) {
    console.log(`IDP selection screen detected. Selecting provider: "${idpName}"`);
    
    //look for the specific idp link without exact matching
    const idpLink = page.getByRole('link', { name: idpName });
    
    await idpLink.waitFor({ state: 'visible', timeout: TIMEOUTS.short });
    await idpLink.click();
  } else {
    console.log("No IDP screen detected (or already selected), proceeding to credentials...");
  }

  //fill in the credentials
  await usernameInput.waitFor({ state: 'visible', timeout: TIMEOUTS.medium });
  await usernameInput.fill(user); 

  const passwordInput = page.getByLabel(/Password/i)
    .or(page.locator('input[name="password"]'))
    .or(page.getByPlaceholder(/Password/i));

  if (!process.env.CLUSTER_PASSWORD) {
      throw new Error("CLUSTER_PASSWORD is not set in the environment!");
  }

  await passwordInput.fill(process.env.CLUSTER_PASSWORD);
  await page.getByRole('button', { name: /Log in/i }).click();

  //handle the openshift welcome tour modal if it appears
  try {
    const skipTourButton = page.getByRole('button', { name: /skip tour/i });
    //wait up to 5 seconds for the modal to pop up
    await skipTourButton.waitFor({ state: 'visible', timeout: TIMEOUTS.short });
    await skipTourButton.click();
    console.log('Dismissed the OpenShift Welcome Tour modal.');
  } catch (error) {
    if (error instanceof Error && error.name === 'TimeoutError') {
      //safely ignore the timeout and move on
      console.log('welcome tour modal did not appear, continuing...');
    } else {
      //throw any other unexpected errors 
      throw error;
    }
  }

  //save the auth state
  await expect(page.getByRole('navigation').first()).toBeVisible({ timeout: TIMEOUTS.long });
  await expect(page).toHaveURL(/(console|k8s|overview|dashboards)/i, { timeout: TIMEOUTS.default });
  await page.context().storageState({ path: authFile });
});