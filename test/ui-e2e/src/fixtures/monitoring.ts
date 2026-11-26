import { test as base, expect } from '@playwright/test';
import { DashboardsPage } from '../pages/console/DashboardsPage';
import { MetricsPage } from '../pages/console/MetricsPage';
import {
  ArgoCDInstance,
  deleteArgoCDInstance,
  resolveArgoCDInstance,
} from '../utils/argocd-instance';

type MonitoringFixtures = {
  dashboardsPage: DashboardsPage;
  metricsPage: MetricsPage;
  userDefinedArgoCD: ArgoCDInstance;
};

export const test = base.extend<MonitoringFixtures>({
  dashboardsPage: async ({ page }, use) => {
    await use(new DashboardsPage(page));
  },

  metricsPage: async ({ page }, use) => {
    await use(new MetricsPage(page));
  },

  //fresh argocd takes ages to get a servicemonitor
  userDefinedArgoCD: [
    async ({}, use) => {
      const instance = await resolveArgoCDInstance();
      try {
        await use(instance);
      } finally {
        deleteArgoCDInstance(instance);
      }
    },
    { timeout: 13 * 60 * 1000 },
  ],
});

export { expect };
export type { ArgoCDInstance };
