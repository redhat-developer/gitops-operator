import { test, expect } from '../src/fixtures';
import { execFileSync } from 'child_process';
import { ApplicationsPage } from '../src/pages/ApplicationsPage';
import { ApplicationDetailsPage } from '../src/pages/ApplicationDetailsPage';

test.describe('Application History and Rollback', () => {
  const stamp = Date.now();
  const appName = `ui-rollback-${stamp}`;
  const destNs = `ui-rollback-ns-${stamp}`;
  //two guestbook commits with different images so rollback leaves live state behind git head
  const previousCommit = '68657670d9131dc5bc5f538b14c1de3377d74591';
  const currentCommit = '8088f4c0d970abb09e250248cc97e35623447cb5';
  const previousImage = 'quay.io/argoprojlabs/argocd-e2e-container:0.2';

  const ocGet = (args: string[]): string =>
    execFileSync('oc', args, { stdio: 'pipe', timeout: 5000 }).toString().trim();

  const applicationExists = (): boolean => {
    const out = ocGet([
      'get', 'application', appName, '-n', 'openshift-gitops', '--ignore-not-found', '-o', 'name',
    ]);
    return out.length > 0;
  };

  const applyApp = (targetRevision: string) => {
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
    targetRevision: ${targetRevision}
`;
    execFileSync('oc', ['apply', '-f', '-'], { input: appYaml, stdio: 'pipe', timeout: 15000 });
  };

  const triggerSync = () => {
    execFileSync(
      'oc',
      [
        'patch', 'application', appName, '-n', 'openshift-gitops',
        '--type', 'merge',
        '-p', '{"operation":{"initiatedBy":{"username":"admin"},"sync":{}}}',
      ],
      { stdio: 'pipe', timeout: 15000 }
    );
  };

  const guestbookImage = (): string =>
    ocGet([
      'get', 'deploy', 'guestbook-ui', '-n', destNs, '--ignore-not-found',
      '-o', 'jsonpath={.spec.template.spec.containers[0].image}',
    ]);

  test.beforeAll(async ({}, testInfo) => {
    testInfo.setTimeout(300000);
    console.log(`\n[setup] Deploying '${appName}' with two guestbook syncs...`);

    execFileSync('oc', ['create', 'namespace', destNs], { stdio: 'pipe', timeout: 15000 });
    execFileSync(
      'oc',
      ['label', 'namespace', destNs, 'argocd.argoproj.io/managed-by=openshift-gitops', '--overwrite'],
      { stdio: 'pipe', timeout: 15000 }
    );

    let rbacReady = false;
    for (let i = 1; i <= 30; i++) {
      const rbs = ocGet(['get', 'rolebinding', '-n', destNs, '-o', 'name']);
      if (/argocd-application-controller/i.test(rbs)) {
        rbacReady = true;
        break;
      }
      await new Promise((r) => setTimeout(r, 2000));
    }
    if (!rbacReady) {
      throw new Error(
        `Namespace '${destNs}' never received openshift-gitops application-controller RoleBinding.`
      );
    }

    applyApp(previousCommit);

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
      console.log(`[setup] compared sync='${lastSync || '-'}' resource='${lastResource || '-'}' (${i}/30)`);
      if (lastSync === 'OutOfSync' && lastResource) break;
      await new Promise((r) => setTimeout(r, 3000));
    }
    if (lastSync !== 'OutOfSync' || !lastResource) {
      throw new Error(
        `Application '${appName}' never reached OutOfSync with compared resources ` +
          `(sync='${lastSync || '-'}' resource='${lastResource || '-'}').`
      );
    }

    triggerSync();

    let firstHealth = '';
    let firstSynced = false;
    for (let i = 1; i <= 40; i++) {
      lastSync = ocGet([
        'get', 'application', appName, '-n', 'openshift-gitops',
        '-o', 'jsonpath={.status.sync.status}',
      ]);
      firstHealth = ocGet([
        'get', 'application', appName, '-n', 'openshift-gitops',
        '-o', 'jsonpath={.status.health.status}',
      ]);
      console.log(`[setup] first sync='${lastSync || '-'}' health='${firstHealth || '-'}' (${i}/40)`);
      if (lastSync === 'Synced' && firstHealth === 'Healthy') {
        firstSynced = true;
        break;
      }
      await new Promise((r) => setTimeout(r, 3000));
    }
    if (!firstSynced) {
      throw new Error(
        `First revision never reached Synced/Healthy ` +
          `(sync='${lastSync || '-'}' health='${firstHealth || '-'}').`
      );
    }

    applyApp(currentCommit);

    let drifted = false;
    for (let i = 1; i <= 30; i++) {
      lastSync = ocGet([
        'get', 'application', appName, '-n', 'openshift-gitops',
        '-o', 'jsonpath={.status.sync.status}',
      ]);
      console.log(`[setup] after bump sync='${lastSync || '-'}' (${i}/30)`);
      if (lastSync === 'OutOfSync') {
        drifted = true;
        break;
      }
      await new Promise((r) => setTimeout(r, 3000));
    }
    if (!drifted) {
      throw new Error(`Application '${appName}' never drifted after bumping targetRevision.`);
    }

    triggerSync();

    let secondSynced = false;
    let history = '';
    for (let i = 1; i <= 40; i++) {
      lastSync = ocGet([
        'get', 'application', appName, '-n', 'openshift-gitops',
        '-o', 'jsonpath={.status.sync.status}',
      ]);
      history = ocGet([
        'get', 'application', appName, '-n', 'openshift-gitops',
        '-o', 'jsonpath={.status.history[*].revision}',
      ]);
      const hasPrevious = history.includes(previousCommit);
      const hasCurrent = history.includes(currentCommit);
      console.log(`[setup] second sync='${lastSync || '-'}' history='${history || '-'}' (${i}/40)`);
      if (lastSync === 'Synced' && hasPrevious && hasCurrent) {
        secondSynced = true;
        break;
      }
      await new Promise((r) => setTimeout(r, 3000));
    }
    if (!secondSynced) {
      throw new Error(
        `Second revision never reached Synced with two history entries ` +
          `(sync='${lastSync || '-'}' history='${history || '-'}').`
      );
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

  test('Rollback to a previous revision from History and Rollback', async ({ page }) => {
    test.setTimeout(240000);

    const appsPage = new ApplicationsPage(page);
    const detailsPage = new ApplicationDetailsPage(page);

    await appsPage.navigate();
    await appsPage.openApplication(appName);
    await detailsPage.openHistoryAndRollback();
    await detailsPage.rollbackToPreviousRevision(appName, previousCommit);

    await expect
      .poll(
        () => {
          const sync = ocGet([
            'get', 'application', appName, '-n', 'openshift-gitops',
            '-o', 'jsonpath={.status.sync.status}',
          ]);
          const health = ocGet([
            'get', 'application', appName, '-n', 'openshift-gitops',
            '-o', 'jsonpath={.status.health.status}',
          ]);
          return `${sync},${health}`;
        },
        { timeout: 180000, message: 'waiting for OutOfSync + Healthy after rollback' }
      )
      .toBe('OutOfSync,Healthy');

    expect(guestbookImage(), 'rollback should restore the previous guestbook image').toBe(previousImage);

    await detailsPage.assertOutOfSyncHealthy();
  });
});
