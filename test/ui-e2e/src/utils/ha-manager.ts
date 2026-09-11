import { execSync } from 'child_process';

const MIN_WORKERS = 3;
const MIN_WORKER_MEM_KI = 12 * 1024 * 1024; // 12Gi

type HaCapacityAssessment = {
  constrained: boolean;
  reasons: string[];
  workerCount: number;
  workerMemSummary: string;
};

//Ki/Mi/Gi -> Ki
function memoryToKi(quantity: string): number {
  const match = /^(\d+(?:\.\d+)?)(Ki|Mi|Gi|Ti)?$/.exec(quantity.trim());
  if (!match) return 0;
  const value = parseFloat(match[1]);
  const unit = match[2] || 'Ki';
  switch (unit) {
    case 'Ti':
      return Math.round(value * 1024 * 1024 * 1024);
    case 'Gi':
      return Math.round(value * 1024 * 1024);
    case 'Mi':
      return Math.round(value * 1024);
    case 'Ki':
    default:
      return Math.round(value);
  }
}

function assessHaCapacity(): HaCapacityAssessment {
  const reasons: string[] = [];
  let workerCount = 0;
  let workerMemSummary = 'unknown';

  if (process.env.REDUCE_HA_RESOURCES === 'true') {
    reasons.push('REDUCE_HA_RESOURCES=true');
  }

  try {
    const raw = execSync('oc get nodes -l node-role.kubernetes.io/worker -o json', { stdio: 'pipe' }).toString();
    const workers = JSON.parse(raw).items || [];
    workerCount = workers.length;
    const memParts: string[] = [];

    for (const node of workers) {
      const name = node.metadata?.name || 'worker';
      const memKi = memoryToKi(node.status?.allocatable?.memory || '0');
      memParts.push(`${name}=${memKi > 0 ? `${Math.round(memKi / 1024 / 1024)}Gi` : '?'}`);
      if (memKi > 0 && memKi < MIN_WORKER_MEM_KI) {
        reasons.push(`${name} allocatable memory ${Math.round(memKi / 1024 / 1024)}Gi < 12Gi`);
      }
    }
    workerMemSummary = memParts.join(', ') || 'none';

    if (workerCount < MIN_WORKERS) {
      reasons.push(`worker count ${workerCount} < ${MIN_WORKERS}`);
    }
  } catch (e: any) {
    //don't treat oc errors as low capacity
    console.warn(`[setup] could not inspect workers (${e?.message || e}); skipping capacity reduction`);
  }

  return {
    constrained: reasons.length > 0,
    reasons,
    workerCount,
    workerMemSummary,
  };
}

export async function enableHA() {
  console.log('\n[setup] Enabling High Availability (HA) for Argo CD...');

  const capacity = assessHaCapacity();
  const reduceResources = capacity.constrained;
  const lowRes = { requests: { cpu: '50m', memory: '64Mi' }, limits: { cpu: '250m', memory: '128Mi' } };
  const patchObj = reduceResources
    ? { spec: { ha: { enabled: true, resources: lowRes }, redis: { resources: lowRes } } }
    : { spec: { ha: { enabled: true } } };

  if (reduceResources) {
    console.warn(
      [
        '[setup] WARNING: cluster may lack capacity for reliable HA (continuing anyway; HA is still required).',
        `         workers=${capacity.workerCount} mem=[${capacity.workerMemSummary}]`,
        `         reasons: ${capacity.reasons.join('; ')}`,
        '         Applying reduced HA/redis resource specs. If redis-ha rollout times out, treat as an environment/capacity issue — not a UI login bug.',
      ].join('\n')
    );
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

  if (!podsReady) {
    const hint = reduceResources
      ? ' ENV_CAPACITY_WARNING: cluster was flagged as constrained before enableHA; prefer investigating redis-ha/capacity over UI selectors.'
      : '';
    throw new Error('HA proxy deployment never appeared or became available after polling.' + hint);
  }

  console.log('[setup] Waiting for Operator to roll out HA-aware components...');
  try {
    execSync('oc rollout status statefulset/openshift-gitops-redis-ha-server -n openshift-gitops --timeout=300s', { stdio: 'inherit' });
    execSync('oc rollout status deployment/openshift-gitops-server -n openshift-gitops --timeout=300s', { stdio: 'inherit' });
    execSync('oc rollout status deployment/openshift-gitops-dex-server -n openshift-gitops --timeout=300s', { stdio: 'inherit' });
  } catch (e: any) {
    if (reduceResources) {
      throw new Error(
        `HA rollout failed after capacity warning (likely environment/capacity, not UI). Underlying: ${e?.message || e}`
      );
    }
    throw e;
  }

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
