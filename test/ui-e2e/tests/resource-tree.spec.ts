import { test } from '../src/fixtures';
import { ApplicationDetailsPage } from '../src/pages/ApplicationDetailsPage';
import { ApplicationsPage } from '../src/pages/ApplicationsPage';

test.describe('Argo CD Resource Tree and Pod Logs', () => {

  test.use({ storageState: '.auth/storageState.json' });

  test('Navigate to app details, open a Pod, and verify logs stream', async ({ page, managedApp }) => {
    test.setTimeout(120000);

    const appsPage = new ApplicationsPage(page);
    const detailsPage = new ApplicationDetailsPage(page);

    await appsPage.navigate();
    await appsPage.openApplication(managedApp);

    await detailsPage.verifyResourceTreeLoaded();
    await detailsPage.clickResourceNode('deploy', 'spring-petclinic');
    await detailsPage.verifyPodLogs();
  });

});
