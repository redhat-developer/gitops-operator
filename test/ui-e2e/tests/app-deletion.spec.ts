import { test, expect } from '../src/fixtures';
import { execFileSync } from 'child_process';

test.describe('Clean Application Deletion (Pruning)', () => {
  //make app name unique for test isolation
  const appName = `ui-deletion-${Date.now()}`;
  //pin revision to immutable commit SHA for reproducibility
  const targetCommit = '8088f4c0d970abb09e250248cc97e35623447cb5';

  //returns true when the application cr is still present
  const applicationExists = (): boolean => {
    const out = execFileSync(
      'oc',
      ['get', 'application', appName, '-n', 'openshift-gitops', '--ignore-not-found', '-o', 'name'],
      { stdio: 'pipe', timeout: 5000 }
    ).toString().trim();
    return out.length > 0;
  };

  //guestbook always creates deploy/svc named guestbook-ui. use --ignore-not-found -o name
  //(empty = gone; real oc errors still throw). avoids instance-label lookups that fail under annotation tracking.
  const remainingChildResources = (): string => {
    const kinds = ['deploy', 'svc'] as const;
    return kinds
      .map((kind) =>
        execFileSync(
          'oc',
          ['get', kind, 'guestbook-ui', '-n', 'openshift-gitops', '--ignore-not-found', '-o', 'name'],
          { stdio: 'pipe', timeout: 5000 }
        ).toString().trim()
      )
      .filter(Boolean)
      .join('\n');
  };

  test.beforeAll(async ({}, testInfo) => {
    //set timeout to 150s (sync poll can take ~90s after a fresh install)
    testInfo.setTimeout(150000);
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
      //clear leftover guestbook children that can leave a new app stuck Unknown/OutOfSync
      for (const kind of ['deploy', 'svc'] as const) {
        execFileSync(
          'oc',
          ['delete', kind, 'guestbook-ui', '-n', 'openshift-gitops', '--ignore-not-found', '--wait=false'],
          { stdio: 'pipe', timeout: 15000 }
        );
      }

      //deploy dummy app via cli with process timeout
      execFileSync('oc', ['apply', '-f', '-'], { input: appYaml, stdio: 'pipe', timeout: 15000 });

      //poll until argo cd reports synced (unknown is common while repo-server warms up)
      let isSynced = false;
      let lastSync = '';
      let lastHealth = '';
      let lastMessage = '';
      for (let i = 1; i <= 30; i++) {
        try {
          lastSync = execFileSync(
            'oc',
            ['get', 'application', appName, '-n', 'openshift-gitops', '-o', 'jsonpath={.status.sync.status}'],
            { stdio: 'pipe', timeout: 3000 }
          ).toString().trim();
          lastHealth = execFileSync(
            'oc',
            ['get', 'application', appName, '-n', 'openshift-gitops', '-o', 'jsonpath={.status.health.status}'],
            { stdio: 'pipe', timeout: 3000 }
          ).toString().trim();
          lastMessage = execFileSync(
            'oc',
            ['get', 'application', appName, '-n', 'openshift-gitops', '-o', 'jsonpath={.status.conditions[0].message}'],
            { stdio: 'pipe', timeout: 3000 }
          ).toString().trim();
          console.log(
            `[setup] Checking sync status (Attempt ${i}/30): sync='${lastSync || 'Initializing...'}' health='${lastHealth || '-'}'`
          );
          if (lastSync === 'Synced') {
            isSynced = true;
            break;
          }
        } catch (e) {
          console.log(`[setup] Checking sync status (Attempt ${i}/30): Waiting for resource...`);
        }
        await new Promise(resolve => setTimeout(resolve, 3000));
      }

      if (!isSynced) {
        throw new Error(
          `Dummy application '${appName}' never reached Synced status ` +
            `(last sync='${lastSync || '-'}' health='${lastHealth || '-'}' message='${lastMessage || '-'}').`
        );
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
      execFileSync(
        'oc',
        ['delete', 'application', appName, '-n', 'openshift-gitops', '--ignore-not-found', '--wait=true'],
        { stdio: 'pipe', timeout: 15000 }
      );
    } catch (e) {
      console.warn(`[teardown] Initial cleanup command failed: ${(e as Error).message}`);
    }

    //verify resource is completely absent from cluster
    let isDeleted = false;
    for (let i = 1; i <= 5; i++) {
      if (!applicationExists()) {
        isDeleted = true;
        break;
      }
      //resource still exists, wait before checking again
      await new Promise(resolve => setTimeout(resolve, 2000));
    }

    if (!isDeleted) {
      throw new Error(`[teardown] Cleanup verification failed: '${appName}' still exists on cluster.`);
    }
  });

  test('Delete application via UI and verify cascading deletion', async ({ page }) => {
    //set explicit test timeout budget
    test.setTimeout(90000);

    //locate application card specifically bound to appName without broad div scanning
    const appTile = page.locator('.application-tile, [class*="application-tile"], [class*="applications-list__entry"]')
      .filter({ hasText: appName });

    //ensure application tile appears on dashboard
    await expect(appTile).toBeVisible({ timeout: 30000 });

    //confirm guestbook children exist before delete so cascade assertion is meaningful
    expect(remainingChildResources()).not.toBe('');

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
      if (!applicationExists()) {
        backendDeleted = true;
        break;
      }
      //resource still present on cluster, wait before checking again
      await new Promise(resolve => setTimeout(resolve, 2000));
    }

    expect(backendDeleted).toBe(true);

    //verify cascading deletion removed guestbook deploy/svc
    let childrenGone = false;
    for (let i = 1; i <= 10; i++) {
      const remaining = remainingChildResources();
      if (remaining === '') {
        childrenGone = true;
        break;
      }
      console.log(`[verify] Waiting for child resources to prune (Attempt ${i}/10): ${remaining}`);
      await new Promise(resolve => setTimeout(resolve, 2000));
    }

    expect(childrenGone).toBe(true);
  });
});
