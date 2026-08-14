import { execFileSync } from 'child_process';

/** Resolve hostname from the Argo CD repo-server (same DNS path as git ls-remote). */
export function clusterCanResolveHostname(hostname: string): boolean {
  if (!hostname) return false;
  try {
    const out = execFileSync(
      'oc',
      [
        'exec',
        '-n',
        'openshift-gitops',
        'deploy/openshift-gitops-repo-server',
        '-c',
        'argocd-repo-server',
        '--',
        'getent',
        'hosts',
        hostname,
      ],
      { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'], timeout: 20000 }
    );
    return out.trim().length > 0;
  } catch {
    return false;
  }
}

export function hostnameFromRepoUrl(repoUrl: string): string {
  try {
    return new URL(repoUrl).hostname;
  } catch {
    return '';
  }
}
