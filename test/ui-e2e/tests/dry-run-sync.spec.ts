import { test, expect } from '../src/fixtures';
import { execFileSync } from 'child_process';
import { ApplicationsPage } from '../src/pages/ApplicationsPage';

test.describe('Dry Run Sync', () => {
  const stamp = Date.now();
  const appName = `ui-dryrun-${stamp}`;
  const destNs = `ui-dryrun-ns-${stamp}`;
  //pin guestbook so 1.20/1.21/1.22 all resolve the same manifests
  const targetCommit = '8088f4c0d970abb09e250248cc97e35623447cb5';

  const ocGet = (args: string[]): string =>
    execFileSync('oc', args, { stdio: 'pipe', timeout: 5000 }).toString().trim();

  const applicationExists = (): boolean => {
    const out = ocGet([
      'get', 'application', appName, '-n', 'openshift-gitops', '--ignore-not-found', '-o', 'name',
    ]);
    return out.length > 0;
  };

  //guestbook live children — must stay absent after a dry run
  const liveGuestbookResources = (): string => {
    const kinds = ['deploy', 'svc'] as const;
    return kinds
      .map((kind) =>
        ocGet(['get', kind, 'guestbook-ui', '-n', destNs, '--ignore-not-found', '-o', 'name'])
      )
      .filter(Boolean)
      .join('\n');
  };

  test.beforeAll(async ({}, testInfo) => {
    testInfo.setTimeout(180000);
    console.log(`\n[setup] Deploying '${appName}' via CLI (manual sync, for dry-run)...`);

    execFileSync('oc', ['create', 'namespace', destNs], { stdio: 'pipe', timeout: 15000 });
    execFileSync(
      'oc',
      ['label', 'namespace', destNs, 'argocd.argoproj.io/managed-by=openshift-gitops', '--overwrite'],
      { stdio: 'pipe', timeout: 15000 }
    );

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

    let lastSync = '';
    let lastResource = '';
    for (let i = 1; i <= 30; i++) {
      lastSync = ocGet([
        'get', 'application', appName, '-n', 'openshift-gitops',
        '-o', 'jsonpath={.status.sync.status}',
      ]);
      lastResource = ocGet([
        'get', 'application', appName, '-n', 'openshift-gitops',
        '-o', 'jsonpath={.status.resources[0].name}',
      ]);
      console.log(`[setup] sync='${lastSync || '-'}' resource='${lastResource || '-'}' (${i}/30)`);
      if (lastSync === 'OutOfSync' && lastResource) break;
      await new Promise((r) => setTimeout(r, 3000));
    }
    if (lastSync !== 'OutOfSync' || !lastResource) {
      throw new Error(
        `Application '${appName}' never reached OutOfSync with compared resources ` +
          `(sync='${lastSync || '-'}' resource='${lastResource || '-'}').`
      );
    }

    expect(liveGuestbookResources(), 'guestbook must not exist before dry-run').toBe('');
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

  test('Dry-run sync from the UI does not change live cluster resources', async ({ page }) => {
    test.setTimeout(180000);

    const appsPage = new ApplicationsPage(page);
    await appsPage.navigate();
    await appsPage.dryRunSynchronize(appName);

    expect(liveGuestbookResources(), 'dry-run must not create guestbook deploy/svc').toBe('');

    const syncStatus = ocGet([
      'get', 'application', appName, '-n', 'openshift-gitops',
      '-o', 'jsonpath={.status.sync.status}',
    ]);
    expect(syncStatus, 'app should stay OutOfSync after dry-run').toBe('OutOfSync');
  });
});
