import { test, expect } from '../src/fixtures';
import { execFileSync } from 'child_process';
import { ApplicationsPage } from '../src/pages/ApplicationsPage';
import { ApplicationDetailsPage } from '../src/pages/ApplicationDetailsPage';

test.describe('Auto-Sync and Self-Healing', () => {
  const stamp = Date.now();
  const appName = `ui-autosync-${stamp}`;
  const destNs = `ui-autosync-ns-${stamp}`;
  const targetCommit = '8088f4c0d970abb09e250248cc97e35623447cb5';

  const ocGet = (args: string[]): string =>
    execFileSync('oc', args, { stdio: 'pipe', timeout: 5000 }).toString().trim();

  const applicationExists = (): boolean => {
    const out = ocGet([
      'get', 'application', appName, '-n', 'openshift-gitops', '--ignore-not-found', '-o', 'name'
    ]);
    return out.length > 0;
  };

  const deleteGuestbookChildren = () => {
    for (const kind of ['deploy', 'svc'] as const) {
      execFileSync(
        'oc',
        ['delete', kind, 'guestbook-ui', '-n', destNs, '--ignore-not-found', '--wait=false'],
        { stdio: 'pipe', timeout: 15000 }
      );
    }
  };

  test.beforeAll(async ({}, testInfo) => {
    testInfo.setTimeout(120000);
    console.log(`\n[setup] Deploying '${appName}' via CLI (manual sync policy)...`);

    execFileSync('oc', ['create', 'namespace', destNs], { stdio: 'pipe', timeout: 15000 });
    deleteGuestbookChildren();

    //manual sync — UI enables automated policy
    const appYaml = `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: ${appName}
  namespace: openshift-gitops
spec:
  destination:
    namespace: ${destNs}
    server: https://kubernetes.default.svc
  project: default
  source:
    path: guestbook
    repoURL: https://github.com/argoproj/argocd-example-apps.git
    targetRevision: ${targetCommit}
`;
    execFileSync('oc', ['apply', '-f', '-'], { input: appYaml, stdio: 'pipe', timeout: 15000 });

    for (let i = 1; i <= 15; i++) {
      if (applicationExists()) break;
      if (i === 15) throw new Error(`Application '${appName}' never appeared`);
      await new Promise((r) => setTimeout(r, 2000));
    }
  });

  test.afterAll(async ({}, testInfo) => {
    testInfo.setTimeout(90000);
    console.log(`\n[teardown] Cleaning up '${appName}' and '${destNs}'...`);

    try {
      execFileSync(
        'oc',
        ['delete', 'application', appName, '-n', 'openshift-gitops', '--ignore-not-found', '--wait=true'],
        { stdio: 'pipe', timeout: 45000 }
      );
    } catch (e) {
      console.warn(`[teardown] app delete failed: ${(e as Error).message}`);
    }

    deleteGuestbookChildren();
    execFileSync(
      'oc',
      ['delete', 'namespace', destNs, '--ignore-not-found', '--wait=false'],
      { stdio: 'pipe', timeout: 15000 }
    );

    let gone = false;
    for (let i = 1; i <= 10; i++) {
      if (!applicationExists()) {
        gone = true;
        break;
      }
      await new Promise((r) => setTimeout(r, 2000));
    }
    if (!gone) {
      throw new Error(`[teardown] '${appName}' still exists on cluster`);
    }
  });

  test('Enable Auto-Sync, Prune, and Self Heal from App Details', async ({ page }) => {
    test.setTimeout(120000);

    const appsPage = new ApplicationsPage(page);
    const detailsPage = new ApplicationDetailsPage(page);

    await appsPage.navigate();
    await appsPage.openApplication(appName);
    await detailsPage.openAppDetailsPanel();
    await detailsPage.enableAutoSyncWithPruneAndSelfHeal(appName);
    await detailsPage.assertAutoSyncPruneSelfHealEnabled();

    await expect
      .poll(
        () => {
          const prune = ocGet([
            'get', 'application', appName, '-n', 'openshift-gitops',
            '-o', 'jsonpath={.spec.syncPolicy.automated.prune}'
          ]);
          const selfHeal = ocGet([
            'get', 'application', appName, '-n', 'openshift-gitops',
            '-o', 'jsonpath={.spec.syncPolicy.automated.selfHeal}'
          ]);
          return `${prune},${selfHeal}`;
        },
        { timeout: 30000, message: 'waiting for prune+selfHeal on Application CR' }
      )
      .toBe('true,true');
  });
});
