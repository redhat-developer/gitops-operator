import { test } from '../src/fixtures';
import { execFileSync } from 'child_process';
import { ApplicationsPage } from '../src/pages/ApplicationsPage';
import { ApplicationDetailsPage } from '../src/pages/ApplicationDetailsPage';

//gitops 1.20–1.23 (argo 3.3–3.6): locators have to stay on shared ui hooks
test.describe('Argo Rollouts UI extension', () => {
  const stamp = Date.now();
  const appName = `ui-rollouts-${stamp}`;
  const destNs = `ui-rollouts-ns-${stamp}`;
  const rolloutName = 'rollouts-demo';
  const gitopsNs = 'openshift-gitops';
  const rolloutManagerName = 'ui-e2e-rollouts';
  //canary with an infinite pause so promote/abort/pause stay on the menu
  const targetRevision = 'v1.8.3';

  let originalEnableRolloutsUi = '';
  let didEnableRolloutsUi = false;

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

  const ocGetRetry = async (args: string[], attempts = 10): Promise<string> => {
    let lastErr: Error | undefined;
    for (let i = 1; i <= attempts; i++) {
      try {
        return ocGet(args);
      } catch (e) {
        lastErr = e as Error;
        console.log(`[oc] get failed (${i}/${attempts}): ${lastErr.message}`);
        await new Promise((r) => setTimeout(r, 2000));
      }
    }
    throw new Error(`oc get failed after ${attempts} attempts: ${lastErr?.message}`);
  };

  const ourRolloutManager = (): string =>
    tryOcGet([
      'get', 'rolloutmanager', '-n', gitopsNs, '-l', `ui-e2e-run=${stamp}`,
      '--ignore-not-found', '-o', 'name',
    ]);

  const applicationExists = (): boolean => {
    const out = ocGet([
      'get', 'application', appName, '-n', 'openshift-gitops', '--ignore-not-found', '-o', 'name',
    ]);
    return out.length > 0;
  };

  const appHasRollout = (): boolean => {
    const raw = tryOcGet(['get', 'application', appName, '-n', 'openshift-gitops', '-o', 'json']);
    if (!raw) return false;
    try {
      const app = JSON.parse(raw) as { status?: { resources?: { kind?: string }[] } };
      return (app.status?.resources || []).some((r) => r.kind === 'Rollout');
    } catch {
      return false;
    }
  };

  const enableRolloutsUi = async () => {
    originalEnableRolloutsUi = await ocGetRetry([
      'get', 'argocd', 'openshift-gitops', '-n', 'openshift-gitops',
      '-o', 'jsonpath={.spec.server.enableRolloutsUI}',
    ]);

    if (originalEnableRolloutsUi !== 'true') {
      execFileSync(
        'oc',
        [
          'patch', 'argocd', 'openshift-gitops', '-n', 'openshift-gitops',
          '--type', 'merge',
          '-p', '{"spec":{"server":{"enableRolloutsUI":true}}}',
        ],
        { stdio: 'pipe', timeout: 15000 }
      );
      didEnableRolloutsUi = true;
    }

    let hasInit = false;
    for (let i = 1; i <= 40; i++) {
      const inits = tryOcGet([
        'get', 'deploy', 'openshift-gitops-server', '-n', 'openshift-gitops',
        '-o', 'jsonpath={.spec.template.spec.initContainers[*].name}',
      ]);
      console.log(`[setup] server initContainers='${inits || '-'}' (${i}/40)`);
      if (/\brollout-extension\b/.test(inits)) {
        hasInit = true;
        break;
      }
      await new Promise((r) => setTimeout(r, 3000));
    }
    if (!hasInit) {
      throw new Error(
        'openshift-gitops-server never gained the rollout-extension initContainer after enableRolloutsUI=true.'
      );
    }

    execFileSync(
      'oc',
      ['rollout', 'status', 'deploy/openshift-gitops-server', '-n', 'openshift-gitops', '--timeout=180s'],
      { stdio: 'pipe', timeout: 200000 }
    );
  };

  const waitForArgoRolloutsDeploy = async () => {
    let rmReady = false;
    let lastReady = '';
    let lastPhase = '';
    for (let i = 1; i <= 40; i++) {
      lastReady = tryOcGet([
        'get', 'deploy', 'argo-rollouts', '-n', gitopsNs, '--ignore-not-found',
        '-o', 'jsonpath={.status.readyReplicas}',
      ]);
      lastPhase = tryOcGet([
        'get', 'rolloutmanager', '-n', gitopsNs,
        '-o', 'jsonpath={.items[*].status.phase}',
      ]);
      console.log(
        `[setup] argo-rollouts readyReplicas='${lastReady || '-'}' rmPhase='${lastPhase || '-'}' (${i}/40)`
      );
      if (lastReady === '1') {
        rmReady = true;
        break;
      }
      await new Promise((r) => setTimeout(r, 3000));
    }
    if (!rmReady) {
      throw new Error(
        `argo-rollouts in '${gitopsNs}' never reached readyReplicas=1 ` +
          `(ready='${lastReady || '-'}' rmPhase='${lastPhase || '-'}').`
      );
    }
  };

  const rolloutManagerWatchesDest = async (): Promise<'reuse' | 'create'> => {
    const raw = await ocGetRetry([
      'get', 'rolloutmanager', '-n', gitopsNs, '--ignore-not-found', '-o', 'json',
    ]);
    try {
      const items = (
        JSON.parse(raw || '{}') as {
          items?: { spec?: { namespaceScoped?: boolean } }[];
        }
      ).items || [];
      if (items.length) {
        if (items.some((rm) => rm.spec?.namespaceScoped !== true)) return 'reuse';
        throw new Error(
          `RolloutManager in '${gitopsNs}' is namespace-scoped and will not watch '${destNs}'.`
        );
      }
    } catch (e) {
      if (!(e instanceof SyntaxError)) throw e;
    }

    const existingDeploy = tryOcGet([
      'get', 'deploy', 'argo-rollouts', '-n', gitopsNs, '--ignore-not-found', '-o', 'name',
    ]);
    if (existingDeploy) {
      const args = tryOcGet([
        'get', 'deploy', 'argo-rollouts', '-n', gitopsNs,
        '-o', 'jsonpath={.spec.template.spec.containers[0].args}',
      ]);
      if (/\b--namespaced\b/.test(args)) {
        throw new Error(
          `argo-rollouts in '${gitopsNs}' is namespaced and will not watch '${destNs}'.`
        );
      }
      return 'reuse';
    }
    return 'create';
  };

  const applyRolloutManager = async () => {
    if ((await rolloutManagerWatchesDest()) === 'reuse') {
      console.log(`[setup] reusing rollouts in ${gitopsNs}`);
      await waitForArgoRolloutsDeploy();
      return;
    }

    //operator only reconciles rolloutmanagers in CLUSTER_SCOPED_ARGO_ROLLOUTS_NAMESPACES
    const rmYaml = `
apiVersion: argoproj.io/v1alpha1
kind: RolloutManager
metadata:
  name: ${rolloutManagerName}
  namespace: ${gitopsNs}
  labels:
    ui-e2e-run: "${stamp}"
spec: {}
`;
    const tryApply = () => {
      try {
        execFileSync('oc', ['apply', '-f', '-'], { input: rmYaml, stdio: 'pipe', timeout: 15000 });
      } catch (e) {
        if ((e as NodeJS.ErrnoException).code !== 'ETIMEDOUT') throw e;
        console.warn('[setup] oc apply rolloutmanager timed out; checking if it landed');
      }
    };

    tryApply();
    for (let i = 1; i <= 20; i++) {
      const got = ourRolloutManager();
      console.log(`[setup] labelled rolloutmanager='${got || '-'}' (${i}/20)`);
      if (got) break;
      if (i === 5 || i === 10) tryApply();
      await new Promise((r) => setTimeout(r, 2000));
    }
    if (!ourRolloutManager()) {
      throw new Error(`RolloutManager '${rolloutManagerName}' never appeared in '${gitopsNs}'.`);
    }

    let crdReady = false;
    for (let i = 1; i <= 40; i++) {
      const crd = tryOcGet(['get', 'crd', 'rollouts.argoproj.io', '--ignore-not-found', '-o', 'name']);
      console.log(`[setup] rollouts crd='${crd || '-'}' (${i}/40)`);
      if (crd) {
        crdReady = true;
        break;
      }
      await new Promise((r) => setTimeout(r, 3000));
    }
    if (!crdReady) {
      throw new Error('Rollout CRD never appeared after creating RolloutManager.');
    }

    await waitForArgoRolloutsDeploy();
  };

  const applyApp = async () => {
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
    path: docs/getting-started/basic
    repoURL: https://github.com/argoproj/argo-rollouts.git
    targetRevision: ${targetRevision}
`;
    const tryApply = () => {
      try {
        execFileSync('oc', ['apply', '-f', '-'], { input: appYaml, stdio: 'pipe', timeout: 15000 });
      } catch (e) {
        if ((e as NodeJS.ErrnoException).code !== 'ETIMEDOUT') throw e;
        console.warn('[setup] oc apply timed out; checking the application spec');
      }
    };

    tryApply();
    for (let i = 1; i <= 20; i++) {
      const got = tryOcGet([
        'get', 'application', appName, '-n', 'openshift-gitops',
        '-o', 'jsonpath={.spec.source.targetRevision}',
      ]);
      console.log(`[setup] spec revision='${got || '-'}' want='${targetRevision}' (${i}/20)`);
      if (got === targetRevision) return;
      if (i === 5 || i === 10) tryApply();
      await new Promise((r) => setTimeout(r, 2000));
    }
    throw new Error(`Application '${appName}' spec never reached targetRevision '${targetRevision}'.`);
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

  test.beforeAll(async ({}, testInfo) => {
    testInfo.setTimeout(600000);
    console.log(`\n[setup] Enabling rollouts UI and deploying '${appName}'...`);

    await enableRolloutsUi();

    execFileSync('oc', ['create', 'namespace', destNs], { stdio: 'pipe', timeout: 15000 });
    execFileSync(
      'oc',
      ['label', 'namespace', destNs, 'argocd.argoproj.io/managed-by=openshift-gitops', '--overwrite'],
      { stdio: 'pipe', timeout: 15000 }
    );

    let rbacReady = false;
    for (let i = 1; i <= 30; i++) {
      const rbs = tryOcGet(['get', 'rolebinding', '-n', destNs, '-o', 'name']);
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

    await applyRolloutManager();
    await applyApp();

    let lastSync = '';
    let lastResource = '';
    for (let i = 1; i <= 30; i++) {
      lastSync = tryOcGet([
        'get', 'application', appName, '-n', 'openshift-gitops',
        '-o', 'jsonpath={.status.sync.status}',
      ]);
      lastResource = tryOcGet([
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

    let syncedWithRollout = false;
    for (let i = 1; i <= 40; i++) {
      lastSync = tryOcGet([
        'get', 'application', appName, '-n', 'openshift-gitops',
        '-o', 'jsonpath={.status.sync.status}',
      ]);
      const hasRollout = appHasRollout();
      console.log(`[setup] sync='${lastSync || '-'}' rollout=${hasRollout} (${i}/40)`);
      if (lastSync === 'Synced' && hasRollout) {
        syncedWithRollout = true;
        break;
      }
      await new Promise((r) => setTimeout(r, 3000));
    }
    if (!syncedWithRollout) {
      throw new Error(
        `Application '${appName}' never reached Synced with a Rollout resource ` +
          `(sync='${lastSync || '-'}').`
      );
    }
  });

  test.afterAll(async ({}, testInfo) => {
    testInfo.setTimeout(180000);
    console.log(`\n[teardown] Cleaning up '${appName}' and '${destNs}'...`);
    const leftovers: string[] = [];

    const tryDelete = (label: string, args: string[], timeout = 15000) => {
      try {
        execFileSync('oc', args, { stdio: 'pipe', timeout });
      } catch (e) {
        console.warn(`[teardown] ${label} failed: ${(e as Error).message}`);
      }
    };

    tryDelete('app delete', [
      'delete', 'application', appName, '-n', 'openshift-gitops', '--ignore-not-found', '--wait=true',
    ], 45000);

    tryDelete('rolloutmanager delete', [
      'delete', 'rolloutmanager', '-n', gitopsNs, '-l', `ui-e2e-run=${stamp}`,
      '--ignore-not-found', '--wait=false',
    ]);

    tryDelete('namespace delete', [
      'delete', 'namespace', destNs, '--ignore-not-found', '--wait=false',
    ]);

    if (didEnableRolloutsUi) {
      try {
        if (originalEnableRolloutsUi === '') {
          execFileSync(
            'oc',
            [
              'patch', 'argocd', 'openshift-gitops', '-n', 'openshift-gitops',
              '--type', 'json',
              '-p', '[{"op":"remove","path":"/spec/server/enableRolloutsUI"}]',
            ],
            { stdio: 'pipe', timeout: 15000 }
          );
        } else {
          execFileSync(
            'oc',
            [
              'patch', 'argocd', 'openshift-gitops', '-n', 'openshift-gitops',
              '--type', 'merge',
              '-p', '{"spec":{"server":{"enableRolloutsUI":false}}}',
            ],
            { stdio: 'pipe', timeout: 15000 }
          );
        }
      } catch (e) {
        console.warn(`[teardown] enableRolloutsUI restore failed: ${(e as Error).message}`);
        leftovers.push('enableRolloutsUI restore');
      }
    }

    let appGone = false;
    for (let i = 1; i <= 10; i++) {
      try {
        if (!applicationExists()) {
          appGone = true;
          break;
        }
      } catch {
        leftovers.push(`application ${appName} (get failed)`);
        appGone = true;
        break;
      }
      await new Promise((r) => setTimeout(r, 2000));
    }
    if (!appGone) leftovers.push(`application ${appName}`);

    let rmGone = false;
    for (let i = 1; i <= 10; i++) {
      try {
        const got = ocGet([
          'get', 'rolloutmanager', '-n', gitopsNs, '-l', `ui-e2e-run=${stamp}`,
          '--ignore-not-found', '-o', 'name',
        ]);
        if (!got) {
          rmGone = true;
          break;
        }
      } catch {
        leftovers.push(`rolloutmanager ui-e2e-run=${stamp} (get failed)`);
        rmGone = true;
        break;
      }
      await new Promise((r) => setTimeout(r, 2000));
    }
    if (!rmGone) leftovers.push(`rolloutmanager ui-e2e-run=${stamp}`);

    let nsPhase = '';
    for (let i = 1; i <= 5; i++) {
      try {
        nsPhase = ocGet([
          'get', 'namespace', destNs, '--ignore-not-found', '-o', 'jsonpath={.status.phase}',
        ]);
      } catch {
        leftovers.push(`namespace ${destNs} (get failed)`);
        nsPhase = 'unknown';
        break;
      }
      if (!nsPhase || nsPhase === 'Terminating') break;
      tryDelete('namespace delete retry', [
        'delete', 'namespace', destNs, '--ignore-not-found', '--wait=false',
      ]);
      await new Promise((r) => setTimeout(r, 2000));
    }
    if (nsPhase && nsPhase !== 'Terminating') leftovers.push(`namespace ${destNs} (${nsPhase})`);

    if (leftovers.length) {
      throw new Error(`[teardown] still present: ${leftovers.join(', ')}`);
    }
  });

  test('Rollouts extension loads and shows the action menu', async ({ page }) => {
    test.setTimeout(480000);

    const extensionErrors: string[] = [];
    page.on('console', (msg) => {
      if (msg.type() !== 'error') return;
      const loc = msg.location();
      extensionErrors.push([msg.text(), loc?.url].filter(Boolean).join('\n'));
    });
    page.on('pageerror', (err) => {
      extensionErrors.push([err.message, err.stack].filter(Boolean).join('\n'));
    });

    const appsPage = new ApplicationsPage(page);
    const detailsPage = new ApplicationDetailsPage(page);

    //reload so we catch the 11604 load error, not leftover fixture login noise
    await appsPage.navigate();
    detailsPage.assertNoRolloutExtensionLoadError(extensionErrors);

    await appsPage.openApplication(appName);
    await detailsPage.openRolloutExtension(rolloutName);
    detailsPage.assertNoRolloutExtensionLoadError(extensionErrors);
    await detailsPage.assertRolloutActionMenu();
  });
});
