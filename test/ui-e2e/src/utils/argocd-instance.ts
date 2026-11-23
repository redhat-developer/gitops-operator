import { execFileSync } from 'child_process';

export type ArgoCDInstance = {
  name: string;
  namespace: string;
  created: boolean;
};

const AVAILABLE_TIMEOUT_MS = 8 * 60 * 1000;
const MONITORING_TIMEOUT_MS = 2 * 60 * 1000;

function oc(args: string[], input?: string): string {
  try {
    return execFileSync('oc', args, {
      encoding: 'utf8',
      input,
      timeout: 60_000,
      stdio: ['pipe', 'pipe', 'pipe'],
    }).trim();
  } catch (error) {
    const stderr = (error as { stderr?: string | Buffer }).stderr?.toString?.() ?? '';
    const message = stderr.trim() || (error instanceof Error ? error.message : 'oc failed');
    throw new Error(`oc ${args.join(' ')} failed: ${message}`);
  }
}

async function poll(read: () => string, wanted: string, timeoutMs: number, message: string) {
  const started = Date.now();
  let last = '';

  while (Date.now() - started < timeoutMs) {
    try {
      last = read();
      if (last === wanted) return;
    } catch (error) {
      last = error instanceof Error ? error.message : String(error);
    }
    await new Promise((resolve) => setTimeout(resolve, 5000));
  }

  throw new Error(`${message}. last=${last}`);
}

function monitoringLabel(namespace: string) {
  return namespace.startsWith('openshift-')
    ? 'openshift.io/cluster-monitoring'
    : 'openshift.io/user-monitoring';
}

function argocdPhase(name: string, namespace: string) {
  const raw = oc(['get', 'argocd', name, '-n', namespace, '-o', 'json']);
  const parsed = JSON.parse(raw) as { status?: { phase?: string } };
  return parsed.status?.phase ?? '';
}

function labelValue(namespace: string, key: string) {
  const raw = oc(['get', 'namespace', namespace, '-o', 'json']);
  const parsed = JSON.parse(raw) as { metadata?: { labels?: Record<string, string> } };
  return parsed.metadata?.labels?.[key] ?? '';
}

async function waitUntilMonitoringReady(instance: ArgoCDInstance) {
  await poll(
    () => argocdPhase(instance.name, instance.namespace),
    'Available',
    AVAILABLE_TIMEOUT_MS,
    `waiting for ArgoCD ${instance.namespace}/${instance.name} to be Available`
  );

  const label = monitoringLabel(instance.namespace);
  await poll(
    () => labelValue(instance.namespace, label),
    'true',
    MONITORING_TIMEOUT_MS,
    `waiting for ${label}=true on namespace ${instance.namespace}`
  );

  await poll(
    () => {
      oc(['get', 'servicemonitor', instance.name, '-n', instance.namespace]);
      return 'ok';
    },
    'ok',
    MONITORING_TIMEOUT_MS,
    `waiting for ServiceMonitor ${instance.namespace}/${instance.name}`
  );
}

function createInstance(): ArgoCDInstance {
  const namespace = `ui-e2e-mon-${Date.now()}`;
  const name = 'argocd';
  const yaml = [
    'apiVersion: argoproj.io/v1beta1',
    'kind: ArgoCD',
    'metadata:',
    `  name: ${name}`,
    `  namespace: ${namespace}`,
    'spec: {}',
    '',
  ].join('\n');

  oc(['create', 'namespace', namespace]);
  try {
    oc(['apply', '-f', '-'], yaml);
  } catch (error) {
    deleteNamespace(namespace);
    throw error;
  }

  return { name, namespace, created: true };
}

function selectedInstance(): ArgoCDInstance | undefined {
  const namespace = process.env.MONITORING_ARGOCD_NAMESPACE;
  if (!namespace) return undefined;

  const name =
    process.env.MONITORING_ARGOCD_NAME ||
    (namespace === 'openshift-gitops' ? 'openshift-gitops' : 'argocd');

  return { name, namespace, created: false };
}

function deleteNamespace(namespace: string) {
  try {
    oc(['delete', 'namespace', namespace, '--wait=false']);
  } catch (error) {
    console.warn(
      `[teardown] could not delete namespace ${namespace}: ${error instanceof Error ? error.message : error}`
    );
  }
}

//MONITORING_ARGOCD_NAMESPACE means reuse that one
export async function resolveArgoCDInstance(): Promise<ArgoCDInstance> {
  const instance = selectedInstance() ?? createInstance();

  try {
    await waitUntilMonitoringReady(instance);
    return instance;
  } catch (error) {
    if (instance.created) deleteNamespace(instance.namespace);
    throw error;
  }
}

export function deleteArgoCDInstance(instance: ArgoCDInstance) {
  if (!instance.created) return;
  deleteNamespace(instance.namespace);
}
