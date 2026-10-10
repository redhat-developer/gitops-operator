import { test, expect } from '../src/fixtures';
import { SettingsRepositoriesPage } from '../src/pages/SettingsRepositoriesPage';
import { clusterCanResolveHostname, hostnameFromRepoUrl } from '../src/utils/cluster-dns';

test.describe('Private Git Repository Connection', () => {
  const repoUrl = process.env.PRIVATE_REPO_URL || '';
  const username = 'x-access-token';
  const token = process.env.PRIVATE_REPO_TOKEN || '';

  test.beforeEach(() => {
    test.skip(!repoUrl || !token, 'requires PRIVATE_REPO_URL and PRIVATE_REPO_TOKEN');

    const host = hostnameFromRepoUrl(repoUrl);
    if (!clusterCanResolveHostname(host)) {
      //skip is expected on clusters without corp/private DNS
      const reason =
        `skipped: this cluster has no DNS for ${host}, so the private-repo test is not run here`;
      console.log(`[private-repo] ${reason}`);
      test.skip(true, reason);
    }
  });

  test.afterEach(async ({ page }, testInfo) => {
    if (!repoUrl || !token || testInfo.status === 'skipped') return;
    console.log('[teardown] removing configured private repository');
    const reposPage = new SettingsRepositoriesPage(page);
    await reposPage.ensureRepoRemoved(repoUrl);
  });

  test('Connect a private HTTPS repository via Settings', async ({ page }) => {
    test.setTimeout(180000);

    const reposPage = new SettingsRepositoriesPage(page);
    await reposPage.navigate();
    await reposPage.connectHttpsRepo(repoUrl, username, token);
    await reposPage.assertConnectionSuccessful(repoUrl);
  });
});
