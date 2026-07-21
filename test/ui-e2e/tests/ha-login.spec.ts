import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import { LoginPage } from '../src/pages/LoginPage';

//run tests together (same ha state setup)
test.describe.configure({ mode: 'serial' });

test.describe('HA Login Verification', () => {

  //force fresh login
  test.use({ storageState: { cookies: [], origins: [] } });

  test.beforeAll(async () => {
    test.setTimeout(600000); //10 mins for ha rollout
    
    console.log('\n[setup] Enabling High Availability (HA) for Argo CD...');
    try {
      //patch cr with strict timeout
      execSync('oc patch argocd openshift-gitops -n openshift-gitops --type=merge -p \'{"spec":{"ha":{"enabled":true}}}\'', { stdio: 'inherit', timeout: 30000 });

      console.log('[setup] Polling cluster for new HA deployment (this may take a few minutes)...');
      let retries = 30; 
      let podsReady = false;

      while (retries > 0 && !podsReady) {
        try {
          execSync('oc wait --for=condition=Available deployment/openshift-gitops-redis-ha-haproxy -n openshift-gitops --timeout=30s', { stdio: 'pipe' });
          podsReady = true;
        } catch (e) {
          console.log(`[setup] HA proxy not provisioned yet. Retrying in 10s... (${retries} attempts left)`);
          await new Promise(resolve => setTimeout(resolve, 10000));
          retries--;
        }
      }

      if (!podsReady) {
        throw new Error('HA proxy deployment never appeared or became available after polling.');
      }

      console.log('[setup] Waiting for Operator to roll out HA-aware components...');
      
      //wait for rollouts
      execSync('oc rollout status statefulset/openshift-gitops-redis-ha-server -n openshift-gitops --timeout=300s', { stdio: 'inherit' });
      execSync('oc rollout status deployment/openshift-gitops-server -n openshift-gitops --timeout=300s', { stdio: 'inherit' });
      execSync('oc rollout status deployment/openshift-gitops-dex-server -n openshift-gitops --timeout=300s', { stdio: 'inherit' });

      console.log('[setup] Rollouts complete. Giving cluster time to stabilize network routes...');
      await new Promise(resolve => setTimeout(resolve, 10000));

      console.log('[setup] HA successfully enabled and stabilized.');
    } catch (error) {
      console.error('[setup] Failed to enable HA. Aborting tests.', error);
      throw error;
    }
  });

  test.afterAll(async () => {
    test.setTimeout(300000); //5 mins for teardown

    console.log('\n[teardown] Disabling High Availability (HA) to restore cluster state...');
    try {
      //disable ha with strict timeout
      execSync('oc patch argocd openshift-gitops -n openshift-gitops --type=merge -p \'{"spec":{"ha":{"enabled":false}}}\'', { stdio: 'inherit', timeout: 30000 });

      //wait for rollbacks
      execSync('oc wait --for=condition=Available deployment/openshift-gitops-server -n openshift-gitops --timeout=300s', { stdio: 'inherit' });
      execSync('oc rollout status deployment/openshift-gitops-dex-server -n openshift-gitops --timeout=300s', { stdio: 'inherit' });
      
      //helper to independently wait for deletions and only ignore NotFound errors
      const waitForDelete = (resource: string) => {
        try {
          execSync(`oc wait --for=delete ${resource} -n openshift-gitops --timeout=300s`, { stdio: 'pipe' });
        } catch (e: any) {
          const stderr = e.stderr ? e.stderr.toString() : '';
          const message = e.message || '';
          if (!stderr.includes('NotFound') && !message.includes('NotFound')) {
            throw e; //rethrow timeouts or api failures
          }
        }
      };

      //wait for ha components to delete independently
      waitForDelete('statefulset/openshift-gitops-redis-ha-server');
      waitForDelete('deployment/openshift-gitops-redis-ha-haproxy');

      console.log('[teardown] Cluster successfully restored to non-HA state.');
    } catch (error) {
      console.error('[teardown] Failed to disable HA during cleanup! Cluster may be in a dirty state.', error);
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

    //fill form
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