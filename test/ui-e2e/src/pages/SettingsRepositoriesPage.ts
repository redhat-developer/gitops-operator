import { Page, expect, Locator } from '@playwright/test';
import { execFileSync, execSync } from 'node:child_process';

export class SettingsRepositoriesPage {
  readonly page: Page;
  readonly connectRepoButton: Locator;

  constructor(page: Page) {
    this.page = page;
    this.connectRepoButton = page
      .getByRole('button', { name: /Connect Repo/i })
      .or(page.getByText('Connect Repo', { exact: true }));
  }

  private passwordField() {
    return this.page.getByLabel(/^Password/i);
  }

  private usernameField() {
    return this.page.getByLabel(/Username/i);
  }

  private slidingPanel(): Locator {
    return this.page.locator('.sliding-panel--opened, .sliding-panel').filter({ visible: true }).first();
  }

  private repoRow(repoUrl: string): Locator {
    return this.page.locator('.argo-table-list__row, .white-box, tr').filter({ hasText: repoUrl }).first();
  }

  //redact to avoid leaking tokens 
  private redact(text: string): string {
    let out = text;
    for (const secret of [
      process.env.PRIVATE_REPO_TOKEN,
      process.env.PRIVATE_REPO_PASSWORD,
      process.env.CLUSTER_PASSWORD,
    ]) {
      if (secret && secret.length > 0) {
        out = out.split(secret).join('<redacted>');
      }
    }
    return out;
  }

  private async clearSecretsFromForm() {
    await this.passwordField().fill('', { timeout: 2000 }).catch(() => undefined);
    await this.usernameField().fill('', { timeout: 2000 }).catch(() => undefined);
  }

  //SSO api delete often 403/415 — remove matching repo secrets via oc
  private deleteRepoSecretsViaOc(repoUrl: string): number {
    let raw: string;
    try {
      raw = execSync('oc get secrets -n openshift-gitops -o json', {
        encoding: 'utf8',
        timeout: 60000,
        stdio: ['ignore', 'pipe', 'pipe'],
      });
    } catch (e) {
      console.warn(`[private-repo] oc get secrets failed: ${(e as Error).message}`);
      return 0;
    }

    const data = JSON.parse(raw) as {
      items?: Array<{
        metadata?: { name?: string; labels?: Record<string, string> };
        data?: Record<string, string>;
      }>;
    };
    const toDelete: string[] = [];
    for (const secret of data.items || []) {
      const name = secret.metadata?.name;
      const labels = secret.metadata?.labels || {};
      const b64 = secret.data || {};
      //repo secrets only
      if (!name || labels['argocd.argoproj.io/secret-type'] !== 'repository') continue;

      let urlVal = '';
      if (b64.url) {
        try {
          urlVal = Buffer.from(b64.url, 'base64').toString('utf8').trim();
        } catch {
          urlVal = '';
        }
      }
      if (urlVal === repoUrl) {
        toDelete.push(name);
      }
    }

    let deleted = 0;
    for (const name of [...new Set(toDelete)]) {
      try {
        execFileSync('oc', ['delete', 'secret', name, '-n', 'openshift-gitops', '--wait=true'], {
          encoding: 'utf8',
          timeout: 60000,
          stdio: ['ignore', 'pipe', 'pipe'],
        });
        deleted += 1;
        console.log(`[private-repo] deleted repo secret via oc (${deleted})`);
      } catch (e) {
        console.warn(`[private-repo] oc delete secret failed: ${(e as Error).message}`);
      }
    }
    return deleted;
  }

  private async tryDeleteRepoViaApi(repoUrl: string): Promise<boolean> {
    const encodings = [
      encodeURIComponent(repoUrl),
      encodeURIComponent(encodeURIComponent(repoUrl)),
    ];
    let lastStatus = 0;
    for (const encoded of encodings) {
      for (const headers of [
        { 'Content-Type': 'application/json' },
        { 'Content-Type': 'application/json', Accept: 'application/json' },
      ]) {
        const response = await this.page.request.delete(`/api/v1/repositories/${encoded}`, {
          headers,
          data: {},
        });
        lastStatus = response.status();
        if (response.ok() || lastStatus === 404) {
          return true;
        }
      }
    }
    console.warn(`[private-repo] api delete unavailable (last status ${lastStatus})`);
    return false;
  }

  private async tryDeleteRepoViaUi(repoUrl: string): Promise<boolean> {
    await this.page.goto('/settings/repos');
    await expect(this.connectRepoButton).toBeVisible({ timeout: 20000 });
    await expect(this.page.getByText('Loading...', { exact: true })).toHaveCount(0, { timeout: 60000 });

    const row = this.repoRow(repoUrl);
    if (!(await row.isVisible().catch(() => false))) {
      return true;
    }

    console.log('[private-repo] removing repo via ui');
    const menuBtn = row
      .getByRole('button')
      .filter({ hasNotText: /Successful|Failed|Unknown/i })
      .last()
      .or(row.locator('button').last());
    await menuBtn.click();

    const disconnect = this.page
      .getByRole('button', { name: /Disconnect|Remove|Delete/i })
      .or(this.page.getByText(/Disconnect|Remove repository|Delete/i))
      .first();
    await expect(disconnect).toBeVisible({ timeout: 10000 });
    await disconnect.click();

    const ok = this.page.locator('[qe-id="argo-popup-ok-button"]').or(
      this.page.getByRole('button', { name: /^(OK|Confirm|Remove|Disconnect)$/i })
    );
    if (await ok.first().isVisible().catch(() => false)) {
      await ok.first().click();
    }

    await expect(row).toBeHidden({ timeout: 30000 });
    return true;
  }

  async ensureRepoRemoved(repoUrl: string): Promise<void> {
    console.log('[private-repo] ensuring repo is fully removed');
    await this.clearSecretsFromForm().catch(() => undefined);

    const cancel = this.slidingPanel().getByRole('button', { name: /^Cancel$/i });
    if (await cancel.isVisible().catch(() => false)) {
      await cancel.click().catch(() => undefined);
    }

    const apiOk = await this.tryDeleteRepoViaApi(repoUrl);
    if (!apiOk) {
      try {
        await this.tryDeleteRepoViaUi(repoUrl);
      } catch (e) {
        console.warn(`[private-repo] ui delete failed: ${(e as Error).message}`);
      }
    }

    this.deleteRepoSecretsViaOc(repoUrl);

    await this.page.goto('/settings/repos');
    await expect(this.connectRepoButton).toBeVisible({ timeout: 20000 });
    await expect(this.page.getByText('Loading...', { exact: true })).toHaveCount(0, { timeout: 60000 });
    await expect(this.page.getByText(repoUrl, { exact: true })).toHaveCount(0, { timeout: 30000 });

    const leftover = this.deleteRepoSecretsViaOc(repoUrl);
    if (leftover > 0) {
      await this.page.reload();
      await expect(this.page.getByText('Loading...', { exact: true })).toHaveCount(0, { timeout: 60000 });
      await expect(this.page.getByText(repoUrl, { exact: true })).toHaveCount(0, { timeout: 30000 });
    }
    console.log('[private-repo] repo cleanup verified');
  }

  async navigate() {
    console.log('[private-repo] opening settings/repos');
    await this.page.goto('/settings/repos');
    await expect(this.connectRepoButton).toBeVisible({ timeout: 20000 });
    await expect(this.page.getByText('Loading...', { exact: true })).toHaveCount(0, { timeout: 60000 });
  }

  private async selectHttpsConnectionMethod() {
    console.log('[private-repo] selecting VIA HTTP/HTTPS');
    await expect(async () => {
      if (!(await this.slidingPanel().isVisible().catch(() => false))) {
        await this.page.goto('/settings/repos?addRepo=true');
        await expect(this.slidingPanel()).toBeVisible({ timeout: 20000 });
      }
      const panel = this.slidingPanel();
      if (await panel.getByText(/CONNECT REPO USING HTTP\/HTTPS/i).isVisible().catch(() => false)) {
        return;
      }

      const methodTrigger = panel.locator('p').filter({ hasText: /VIA\s+/i }).first();
      await expect(methodTrigger).toBeVisible({ timeout: 5000 });
      await methodTrigger.click();

      const httpsOption = this.page
        .locator('.argo-dropdown__content li')
        .filter({ hasText: /VIA\s*HTTP\/HTTPS/i })
        .first();
      await expect(httpsOption).toBeVisible({ timeout: 5000 });
      //portaled dropdown options often never become Playwright-stable
      await httpsOption.click({ force: true });

      await expect(this.slidingPanel().getByText(/CONNECT REPO USING HTTP\/HTTPS/i)).toBeVisible({
        timeout: 5000,
      });
    }).toPass({ timeout: 45000, intervals: [500, 1000, 2000] });
  }

  async connectHttpsRepo(repoUrl: string, username: string, password: string) {
    await this.ensureRepoRemoved(repoUrl);

    console.log('[private-repo] opening connect repo panel');
    await this.page.goto('/settings/repos?addRepo=true');
    await expect(this.page).toHaveURL(/addRepo=true/, { timeout: 10000 });
    await expect(this.slidingPanel()).toBeVisible({ timeout: 20000 });
    await expect(this.slidingPanel().getByText(/Choose your connection method/i)).toBeVisible({
      timeout: 15000,
    });

    await this.selectHttpsConnectionMethod();
    await expect(this.slidingPanel().getByText(/CONNECT REPO USING HTTP\/HTTPS/i)).toBeVisible({
      timeout: 10000,
    });

    console.log('[private-repo] filling https form');
    const repoUrlField = this.slidingPanel()
      .getByLabel(/Repository URL/i)
      .or(this.slidingPanel().getByPlaceholder(/https:\/\/github\.com/i));
    await repoUrlField.fill(repoUrl);
    if (username) {
      await this.usernameField().fill(username);
    }
    await this.passwordField().fill(password);

    //oauth2/token often needs force basic auth
    const forceBasic = this.slidingPanel().getByLabel(/Force HTTP basic auth/i);
    if (await forceBasic.isVisible().catch(() => false)) {
      if (!(await forceBasic.isChecked())) {
        console.log('[private-repo] enabling force HTTP basic auth');
        await forceBasic.check();
      }
    }

    //internal CA not trusted by default
    const skipTls = this.slidingPanel().getByLabel(/Skip server verification/i);
    if (await skipTls.isVisible().catch(() => false)) {
      if (!(await skipTls.isChecked())) {
        console.log('[private-repo] enabling skip server verification');
        await skipTls.check();
      }
    }

    console.log('[private-repo] clicking connect');
    await this.slidingPanel().getByRole('button', { name: /^Connect$/i }).click();
  }

  private async repoPresentInApi(repoUrl: string): Promise<boolean> {
    try {
      const response = await this.page.request.get('/api/v1/repositories');
      if (!response.ok()) return false;
      const body = (await response.json()) as { items?: Array<{ repo?: string; url?: string }> };
      return (body.items || []).some((item) => item.repo === repoUrl || item.url === repoUrl);
    } catch {
      return false;
    }
  }

  private async visibleConnectionError(): Promise<string> {
    //real failure toasts only
    const failureBanner = this.page
      .getByText(/Unable to connect HTTPS repository/i)
      .or(this.page.getByText(/Unable to connect repository/i))
      .or(this.page.getByText(/Failed to connect/i))
      .first();
    if (await failureBanner.isVisible().catch(() => false)) {
      const detail = (
        await this.page
          .locator('.notifications-list, .toast, [class*="notification"], .argo-notifications')
          .first()
          .innerText()
          .catch(async () => failureBanner.innerText().catch(() => ''))
      ).trim();
      return detail || 'Unable to connect repository';
    }
    return '';
  }

  async assertConnectionSuccessful(repoUrl: string) {
    console.log('[private-repo] waiting for successful connection (max 90s)');
    const row = this.repoRow(repoUrl);
    const deadline = Date.now() + 90000;

    try {
      while (Date.now() < deadline) {
        const err = await this.visibleConnectionError();
        if (err) {
          throw new Error(this.redact(`argo failed to connect private repo: ${err}`));
        }

        if (await row.isVisible().catch(() => false)) {
          await expect(row.getByText(/Successful/i)).toBeVisible({ timeout: 30000 });
          console.log('[private-repo] connection successful');
          return;
        }

        if (await this.repoPresentInApi(repoUrl)) {
          console.log('[private-repo] repo present via API; refreshing list');
          const refresh = this.page.getByRole('button', { name: /Refresh list/i });
          if (await refresh.isVisible().catch(() => false)) {
            await refresh.click();
          } else {
            await this.page.reload();
          }
          await expect(this.page.getByText('Loading...', { exact: true })).toHaveCount(0, {
            timeout: 60000,
          });
          continue;
        }

        //refresh if list stayed empty
        const empty = this.page.getByText(/No repositories connected/i);
        if (await empty.isVisible().catch(() => false)) {
          const refresh = this.page.getByRole('button', { name: /Refresh list/i });
          if (await refresh.isVisible().catch(() => false)) {
            await refresh.click();
            await expect(this.page.getByText('Loading...', { exact: true })).toHaveCount(0, {
              timeout: 30000,
            });
          }
        }

        await new Promise((r) => setTimeout(r, 2000));
      }

      const err = await this.visibleConnectionError();
      const inApi = await this.repoPresentInApi(repoUrl);
      throw new Error(
        this.redact(
          `private repo did not appear in Settings after Connect ` +
            `(apiHasRepo=${inApi}${err ? `; uiError=${err}` : ''}).`
        )
      );
    } finally {
      await this.clearSecretsFromForm();
    }
  }
}
