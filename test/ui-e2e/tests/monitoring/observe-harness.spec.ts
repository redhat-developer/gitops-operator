import { test, expect } from '../../src/fixtures/monitoring';

test.describe('Observe harness', () => {
  test('opens Observe dashboards and selects one from the dropdown', async ({ dashboardsPage }) => {
    await dashboardsPage.open();

    const titles = await dashboardsPage.listDashboards();
    expect(titles.length).toBeGreaterThan(0);

    await dashboardsPage.selectDashboard(titles[0]);
    await expect.poll(async () => dashboardsPage.selectedDashboard()).toContain(titles[0]);
  });

  test('opens administrator Observe metrics', async ({ metricsPage }) => {
    await metricsPage.openAdmin();
  });

  test('opens developer Observe metrics', async ({ metricsPage }) => {
    const namespace = process.env.MONITORING_ARGOCD_NAMESPACE || 'openshift-gitops';
    await metricsPage.openDeveloper(namespace);
  });
});
