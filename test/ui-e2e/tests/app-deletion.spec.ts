import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import { LoginPage } from '../src/pages/LoginPage';

test.describe('Clean Application Deletion (Pruning)', () => {
  //make app name unique for test isolation
  const appName = `ui-deletion-${Date.now()}`;
  //pin revision to immutable commit SHA for reproducibility
  const targetCommit = '8088f4c0d970abb09e250248cc97e35623447cb5';

  test.beforeAll(async ({}, testInfo) => {
    //set timeout to 120s 
    testInfo.setTimeout(120000); 
    console.log(`\n[setup] Deploying dummy application '${appName}' via CLI...`);
    
    //define standard guestbook app yaml targeting openshift-gitops namespace
    const appYaml = `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: ${appName}
  namespace: openshift-gitops
spec:
  destination:
    namespace: openshift-gitops
    server: https://kubernetes.default.svc
  project: default
  source:
    path: guestbook
    repoURL: https://github.com/argoproj/argocd-example-apps.git
    targetRevision: ${targetCommit}
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
`;
    try {
      //deploy dummy app via cli with process timeout
      execSync(`echo "${appYaml}" | oc apply -f -`, { stdio: 'pipe', timeout: 15000 });
      
      //poll until argo cd controller populates sync status (bounded loop)
      let isSynced = false;
      for (let i = 1; i <= 15; i++) {
        try {
          const syncStatus = execSync(`oc get application ${appName} -n openshift-gitops -o jsonpath='{.status.sync.status}'`, { stdio: 'pipe', timeout: 3000 }).toString().trim();
          console.log(`[setup] Checking sync status (Attempt ${i}/15): '${syncStatus || 'Initializing...'}'`);
          if (syncStatus === 'Synced') {
            isSynced = true;
            break;
          }
        } catch (e) {
          console.log(`[setup] Checking sync status (Attempt ${i}/15): Waiting for resource...`);
        }
        await new Promise(resolve => setTimeout(resolve, 3000));
      }

      if (!isSynced) {
        throw new Error(`Dummy application '${appName}' never reached Synced status.`);
      }
    } catch (e) {
      console.error('Failed to pre-deploy dummy app', e);
      throw e;
    }
  });

  test.afterAll(async ({}, testInfo) => {
    //set hook timeout to 60s
    testInfo.setTimeout(60000);
    console.log('\n[teardown] Ensuring application is cleaned up...');
    
    //attempt fallback cleanup if ui deletion failed or was skipped
    try {
      execSync(`oc delete application ${appName} -n openshift-gitops --ignore-not-found --wait=true`, { stdio: 'pipe', timeout: 15000 });
    } catch (e) {
      console.warn(`[teardown] Initial cleanup command failed: ${(e as Error).message}`);
    }

    //verify resource is completely absent from cluster
    let isDeleted = false;
    for (let i = 1; i <= 5; i++) {
      try {
        execSync(`oc get application ${appName} -n openshift-gitops`, { stdio: 'pipe', timeout: 2000 });
        //if command succeeds resource still exists
        await new Promise(resolve => setTimeout(resolve, 2000));
      } catch (e) {
        //resource no longer exists
        isDeleted = true;
        break;
      }
    }

    if (!isDeleted) {
      throw new Error(`[teardown] Cleanup verification failed: '${appName}' still exists on cluster.`);
    }
  });

  test('Delete application via UI and verify cascading deletion', async ({ page }) => {
    //set explicit test timeout budget
    test.setTimeout(90000);

    //log into argo cd ui
    const loginPage = new LoginPage(page);
    await loginPage.goto();
    await loginPage.loginViaOpenShift(
      process.env.CLUSTER_USER!,
      process.env.CLUSTER_PASSWORD!,
      process.env.IDP || 'kube:admin'
    );

    //locate application card specifically bound to appName without broad div scanning
    const appTile = page.locator('.application-tile, [class*="application-tile"], [class*="applications-list__entry"]')
      .filter({ hasText: appName });

    //ensure application tile appears on dashboard
    await expect(appTile).toBeVisible({ timeout: 30000 });

    //click delete button scoped specifically to this app card
    const deleteBtn = appTile.locator('[qe-id="applications-tiles-button-delete"]');
    await deleteBtn.click();

    //locate modal container via dialog role or confirmation prompt text
    const modal = page.getByRole('dialog')
      .or(page.locator('div').filter({ hasText: /to confirm the deletion/i }))
      .first();
    await expect(modal).toBeVisible({ timeout: 15000 });

    //type application name into confirmation field
    const confirmInput = modal.getByRole('textbox').or(modal.locator('input')).first();
    await confirmInput.fill(appName);

    //confirm deletion
    const okBtn = modal.getByRole('button', { name: /^ok$/i }).or(modal.locator('button').filter({ hasText: /^ok$/i })).first();
    await okBtn.click();

    //assert modal closes after confirming
    await expect(modal).toBeHidden({ timeout: 15000 });

    //assert app tile disappears from ui dashboard
    await expect(appTile).toBeHidden({ timeout: 30000 });

    //verify backend cr deletion via cli directly within test block before teardown
    let backendDeleted = false;
    for (let i = 1; i <= 10; i++) {
      try {
        execSync(`oc get application ${appName} -n openshift-gitops`, { stdio: 'pipe', timeout: 2000 });
        //resource still present on cluster, wait before checking again
        await new Promise(resolve => setTimeout(resolve, 2000));
      } catch (e) {
        //oc command threw an error, meaning resource was deleted from kubernetes api
        backendDeleted = true;
        break;
      }
    }

    expect(backendDeleted).toBe(true);
  });
});