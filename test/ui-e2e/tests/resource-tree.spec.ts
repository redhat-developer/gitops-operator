import { test, expect } from '../src/fixtures'; 
import { ApplicationDetailsPage } from '../src/pages/ApplicationDetailsPage';
import { ApplicationsPage } from '../src/pages/ApplicationsPage';

test.describe('Argo CD Resource Tree and Pod Logs', () => {

  test.use({ storageState: '.auth/storageState.json' });

  test('Navigate to app details, open a Pod, and verify logs stream', async ({ page, managedApp, argoVersion }) => {
    test.setTimeout(120000); 

    const appsPage = new ApplicationsPage(page);
    const detailsPage = new ApplicationDetailsPage(page);
    
    await appsPage.navigate();
    await page.getByPlaceholder(/Search applications/i).fill(managedApp);
    
    //click the Application Name text/link
    const appCard = page.locator('.white-box, .argo-table-list__row').filter({ hasText: managedApp });
    await appCard.getByText(managedApp, { exact: true }).first().click();

    //on details page
    await detailsPage.verifyResourceTreeLoaded();
    //Deployment node 
    await detailsPage.clickResourceNode('deploy', 'spring-petclinic');
    await detailsPage.verifyPodLogs();
  });

});