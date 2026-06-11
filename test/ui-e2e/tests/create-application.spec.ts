import { test, expect } from '../src/fixtures'; 
import { ApplicationsPage } from '../src/pages/ApplicationsPage';

test.describe('ArgoCD Create Application', () => {
  //declare appname 
  let appName: string;

test.afterEach(async ({ page }) => {
    if (!appName) return;

    console.log(`cleaning up: deleting ${appName} via api`);
    
    const response = await page.request.delete(`/api/v1/applications/${appName}?cascade=true`, {
      headers: { 'Content-Type': 'application/json' }
    });
    
    //ignore 404 (already gone) or 403 (no permission on this cluster)
    if (response.status() === 404 || response.status() === 403) {
      if (response.status() === 403) console.log('warning: rbac bypass on this cluster');
      return;
    }

    //only fail for actual server errors
    expect(response.status()).toBeLessThan(400); 
  });

  test('Deploy the Spring Petclinic application via UI', async ({ page }) => {
    test.setTimeout(180000); 
    
    const appsPage = new ApplicationsPage(page);
    appName = `spring-petclinic-${Date.now()}`; 
    const publicRepo = 'https://github.com/redhat-developer/openshift-gitops-getting-started.git';
    const repoPath = 'app';

    await appsPage.navigate();
    await appsPage.createApp(appName, publicRepo, repoPath);
    await appsPage.syncApplication(appName);
    await appsPage.verifyStatus(appName);
  });
  
});