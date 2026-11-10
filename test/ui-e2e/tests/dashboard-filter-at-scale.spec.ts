import { test } from '../src/fixtures';
import { execFileSync } from 'child_process';
import { ApplicationsPage } from '../src/pages/ApplicationsPage';

test.describe('Dashboard filtering at scale', () => {
  const stamp = Date.now();
  const targetCommit = '8088f4c0d970abb09e250248cc97e35623447cb5';
  const prodHealthy = `ui-filt-prod-ok-${stamp}`;
  const devHealthy = `ui-filt-dev-ok-${stamp}`;
  const prodOutOfSync = `ui-filt-prod-oos-${stamp}`;
  const prodHealthyNs = `ui-filt-prod-ok-ns-${stamp}`;
  const devHealthyNs = `ui-filt-dev-ok-ns-${stamp}`;
  const prodOutOfSyncNs = `ui-filt-prod-oos-ns-${stamp}`;

  const ocGet = (args: string[]): string =>
    execFileSync('oc', args, { stdio: 'pipe', timeout: 5000 }).toString().trim();

  //failed oc read → empty so this attempt stays not-ready
  const tryOcGet = (args: string[]): string => {
    try {
      return ocGet(args);
    } catch {
      return '';
    }
  };

  const applicationExists = (name: string): boolean =>
    tryOcGet([
      'get', 'application', name, '-n', 'openshift-gitops', '--ignore-not-found', '-o', 'name',
    ]).length > 0;

  const createNamespace = async (ns: string) => {
    execFileSync('oc', ['create', 'namespace', ns], { stdio: 'pipe', timeout: 15000 });
    execFileSync(
      'oc',
      ['label', 'namespace', ns, 'argocd.argoproj.io/managed-by=openshift-gitops', '--overwrite'],
      { stdio: 'pipe', timeout: 15000 }
    );

    for (let i = 1; i <= 30; i++) {
      const rbs = tryOcGet(['get', 'rolebinding', '-n', ns, '-o', 'name']);
      if (/argocd-application-controller/i.test(rbs)) return;
      await new Promise((r) => setTimeout(r, 2000));
    }
    throw new Error(`Namespace '${ns}' never received openshift-gitops application-controller RoleBinding.`);
  };

  const applyApp = (name: string, destNs: string, env: string) => {
    const appYaml = `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: ${name}
  namespace: openshift-gitops
  labels:
    ui-env: ${env}
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
  };

  const triggerSync = (name: string) => {
    execFileSync(
      'oc',
      [
        'patch', 'application', name, '-n', 'openshift-gitops',
        '--type', 'merge',
        '-p', '{"operation":{"initiatedBy":{"username":"admin"},"sync":{}}}',
      ],
      { stdio: 'pipe', timeout: 15000 }
    );
  };

  const waitForCompared = async (name: string) => {
    let lastSync = '';
    let lastResource = '';
    for (let i = 1; i <= 30; i++) {
      lastSync = tryOcGet([
        'get', 'application', name, '-n', 'openshift-gitops',
        '-o', 'jsonpath={.status.sync.status}',
      ]);
      lastResource = tryOcGet([
        'get', 'application', name, '-n', 'openshift-gitops',
        '-o', 'jsonpath={.status.resources[0].name}',
      ]);
      console.log(`[setup] ${name} compared sync='${lastSync || '-'}' resource='${lastResource || '-'}' (${i}/30)`);
      if (lastSync === 'OutOfSync' && lastResource) return;
      await new Promise((r) => setTimeout(r, 3000));
    }
    throw new Error(
      `Application '${name}' never reached OutOfSync with compared resources ` +
        `(sync='${lastSync || '-'}' resource='${lastResource || '-'}').`
    );
  };

  const waitForSynced = async (name: string) => {
    let lastSync = '';
    let lastHealth = '';
    for (let i = 1; i <= 40; i++) {
      lastSync = tryOcGet([
        'get', 'application', name, '-n', 'openshift-gitops',
        '-o', 'jsonpath={.status.sync.status}',
      ]);
      lastHealth = tryOcGet([
        'get', 'application', name, '-n', 'openshift-gitops',
        '-o', 'jsonpath={.status.health.status}',
      ]);
      console.log(`[setup] ${name} sync='${lastSync || '-'}' health='${lastHealth || '-'}' (${i}/40)`);
      //filters only need synced — health can stay progressing while pods start
      if (lastSync === 'Synced' && lastHealth) return;
      await new Promise((r) => setTimeout(r, 3000));
    }
    throw new Error(
      `Application '${name}' never reached Synced ` +
        `(sync='${lastSync || '-'}' health='${lastHealth || '-'}').`
    );
  };

  test.beforeAll(async ({}, testInfo) => {
    testInfo.setTimeout(600000);
    console.log(`\n[setup] Seeding labelled guestbook apps for dashboard filters...`);

    await createNamespace(prodHealthyNs);
    await createNamespace(devHealthyNs);
    await createNamespace(prodOutOfSyncNs);

    applyApp(prodHealthy, prodHealthyNs, 'prod');
    applyApp(devHealthy, devHealthyNs, 'dev');
    applyApp(prodOutOfSync, prodOutOfSyncNs, 'prod');

    await waitForCompared(prodHealthy);
    await waitForCompared(devHealthy);
    await waitForCompared(prodOutOfSync);

    triggerSync(prodHealthy);
    triggerSync(devHealthy);
    await waitForSynced(prodHealthy);
    await waitForSynced(devHealthy);
  });

  test.afterAll(async ({}, testInfo) => {
    testInfo.setTimeout(180000);
    console.log(`\n[teardown] Cleaning up filter-at-scale apps...`);

    for (const name of [prodHealthy, devHealthy, prodOutOfSync]) {
      try {
        execFileSync(
          'oc',
          ['delete', 'application', name, '-n', 'openshift-gitops', '--ignore-not-found', '--wait=true'],
          { stdio: 'pipe', timeout: 45000 }
        );
      } catch (e) {
        console.warn(`[teardown] app delete failed for ${name}: ${(e as Error).message}`);
      }
    }

    for (const ns of [prodHealthyNs, devHealthyNs, prodOutOfSyncNs]) {
      try {
        execFileSync(
          'oc',
          ['delete', 'namespace', ns, '--ignore-not-found', '--wait=false'],
          { stdio: 'pipe', timeout: 15000 }
        );
      } catch (e) {
        console.warn(`[teardown] namespace delete failed for ${ns}: ${(e as Error).message}`);
      }
    }

    for (const name of [prodHealthy, devHealthy, prodOutOfSync]) {
      let gone = false;
      for (let i = 1; i <= 10; i++) {
        if (!applicationExists(name)) {
          gone = true;
          break;
        }
        await new Promise((r) => setTimeout(r, 2000));
      }
      if (!gone) {
        throw new Error(`[teardown] '${name}' still exists on cluster`);
      }
    }
  });

  test('Filters and search hide non-matching application tiles', async ({ page }) => {
    test.setTimeout(180000);

    const appsPage = new ApplicationsPage(page);
    await appsPage.navigate();
    await appsPage.clearSearch();
    await appsPage.clearAllFilters();

    await appsPage.assertAppVisible(prodHealthy);
    await appsPage.assertAppVisible(devHealthy);
    await appsPage.assertAppVisible(prodOutOfSync);

    await appsPage.filterByLabel('ui-env=prod');
    await appsPage.assertAppVisible(prodHealthy);
    await appsPage.assertAppVisible(prodOutOfSync);
    await appsPage.assertAppHidden(devHealthy);

    await appsPage.clearAllFilters();
    await appsPage.filterByLabel('ui-env=dev');
    await appsPage.assertAppVisible(devHealthy);
    await appsPage.assertAppHidden(prodHealthy);
    await appsPage.assertAppHidden(prodOutOfSync);

    await appsPage.clearAllFilters();
    await appsPage.selectFilterOption('SYNC STATUS', 'Synced');
    await appsPage.assertAppVisible(prodHealthy);
    await appsPage.assertAppVisible(devHealthy);
    await appsPage.assertAppHidden(prodOutOfSync);

    await appsPage.clearAllFilters();
    await appsPage.selectFilterOption('HEALTH STATUS', 'Degraded');
    await appsPage.assertAppHidden(prodHealthy);
    await appsPage.assertAppHidden(devHealthy);
    await appsPage.assertAppHidden(prodOutOfSync);

    await appsPage.clearAllFilters();
    const healthOf = (name: string) =>
      tryOcGet([
        'get', 'application', name, '-n', 'openshift-gitops',
        '-o', 'jsonpath={.status.health.status}',
      ]);
    const applyProdHealth = async () => {
      const health = healthOf(prodHealthy);
      if (!health) {
        throw new Error(`Application '${prodHealthy}' has no health status`);
      }
      await appsPage.selectFilterOption('HEALTH STATUS', health);
      return health;
    };
    //guestbook can flip progressing/healthy between the oc read and the tiles
    let prodHealth = await applyProdHealth();
    try {
      await appsPage.assertAppVisible(prodHealthy);
    } catch {
      await appsPage.clearAllFilters();
      prodHealth = await applyProdHealth();
      await appsPage.assertAppVisible(prodHealthy);
    }
    if (healthOf(prodOutOfSync) !== prodHealth) {
      await appsPage.assertAppHidden(prodOutOfSync);
    }

    await appsPage.clearAllFilters();
    await appsPage.searchApplications(prodHealthy);
    await appsPage.assertAppVisible(prodHealthy);
    await appsPage.assertAppHidden(devHealthy);
    await appsPage.assertAppHidden(prodOutOfSync);
  });
});
