import { execSync } from 'child_process';

//private helper test file doesn't need to see it
function isClusterResourceConstrained(): boolean {
  if (process.env.REDUCE_HA_RESOURCES === 'true') return true;
  try {
    const raw = execSync('oc get nodes -l node-role.kubernetes.io/worker -o json', { stdio: 'pipe' }).toString();
    const workers = JSON.parse(raw).items || [];
    if (workers.length < 3) return true;
    for (const node of workers) {
      const memKi = parseInt(node.status?.allocatable?.memory || '0', 10);
      if (memKi > 0 && memKi < 12 * 1024 * 1024) return true;
    }
  } catch {}
  return false;
}

export async function enableHA() {
  console.log('\n[setup] Enabling High Availability (HA) for Argo CD...');
  
  const reduceResources = isClusterResourceConstrained();
  const lowRes = { requests: { cpu: '50m', memory: '64Mi' }, limits: { cpu: '250m', memory: '128Mi' } };
  const patchObj = reduceResources
    ? { spec: { ha: { enabled: true, resources: lowRes }, redis: { resources: lowRes } } }
    : { spec: { ha: { enabled: true } } };

  if (reduceResources) {
    console.log('[setup] Resource-constrained cluster detected. Applying reduced specs for HA patch...');
  }

  execSync(`oc patch argocd openshift-gitops -n openshift-gitops --type=merge -p '${JSON.stringify(patchObj)}'`, { stdio: 'inherit', timeout: 30000 });

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

  if (!podsReady) throw new Error('HA proxy deployment never appeared or became available after polling.');

  console.log('[setup] Waiting for Operator to roll out HA-aware components...');
  execSync('oc rollout status statefulset/openshift-gitops-redis-ha-server -n openshift-gitops --timeout=300s', { stdio: 'inherit' });
  execSync('oc rollout status deployment/openshift-gitops-server -n openshift-gitops --timeout=300s', { stdio: 'inherit' });
  execSync('oc rollout status deployment/openshift-gitops-dex-server -n openshift-gitops --timeout=300s', { stdio: 'inherit' });

  console.log('[setup] Rollouts complete. Giving cluster time to stabilize network routes...');
  await new Promise(resolve => setTimeout(resolve, 10000));
  console.log('[setup] HA successfully enabled and stabilized.');
}

export async function disableHA() {
  console.log('\n[teardown] Disabling High Availability (HA) to restore cluster state...');
  
  //setting resources to get rid of any overrides from the setup
  const disablePatch = { spec: { ha: { enabled: false, resources: null }, redis: { resources: null } } };
  execSync(`oc patch argocd openshift-gitops -n openshift-gitops --type=merge -p '${JSON.stringify(disablePatch)}'`, { stdio: 'inherit', timeout: 30000 });

  console.log('[teardown] Waiting for Operator to begin reconciliation...');
  await new Promise(resolve => setTimeout(resolve, 15000));

  execSync('oc wait --for=condition=Available deployment/openshift-gitops-server -n openshift-gitops --timeout=300s', { stdio: 'inherit' });
  execSync('oc rollout status deployment/openshift-gitops-dex-server -n openshift-gitops --timeout=300s', { stdio: 'inherit' });
  execSync('oc rollout status deployment/openshift-gitops-redis -n openshift-gitops --timeout=300s', { stdio: 'inherit' });

  const waitForDelete = (resource: string) => {
    try {
      execSync(`oc wait --for=delete ${resource} -n openshift-gitops --timeout=300s`, { stdio: 'pipe' });
    } catch (e: any) {
      if (!e.message?.includes('NotFound') && !e.stderr?.toString().includes('NotFound')) throw e;
    }
  };

  waitForDelete('statefulset/openshift-gitops-redis-ha-server');
  waitForDelete('deployment/openshift-gitops-redis-ha-haproxy');

  await new Promise(resolve => setTimeout(resolve, 5000));
  console.log('[teardown] Cluster successfully restored to non-HA state.');
}