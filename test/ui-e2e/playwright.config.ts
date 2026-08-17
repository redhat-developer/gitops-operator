import { defineConfig, devices } from '@playwright/test';
import dotenv from 'dotenv';
import path from 'path';

/**
 * Read environment variables from file.
 * https://github.com/motdotla/dotenv
 */
dotenv.config({ path: path.resolve(__dirname, '.env') });

/**
 * See https://playwright.dev/docs/test-configuration.
 */
export default defineConfig({
  //register pre-flight script
  globalSetup: require.resolve('./global.setup.ts'),
  //global test timeout to 5 min
  timeout: 5 * 60 * 1000,
  
  testDir: './tests',
  /* Turn off parallel execution inside files */
  fullyParallel: false,
  /* Fail the build on CI if you accidentally left test.only in the source code. */
  forbidOnly: !!process.env.CI,
  /* Retry on CI only */
  retries: process.env.CI ? 2 : 0,
  
  //stops parallel execution so they don't fight over the openshift-gitops namespace.
  workers: 1, 

  /* Reporter to use. See https://playwright.dev/docs/test-reporters */
  reporter: [
    ['list'],
    ['html', { open: process.env.CI ? 'never' : 'on-failure' }]
  ],

  /* GLOBAL FOUNDATION: These apply to everything */
  use: {
    baseURL: process.env.ARGOCD_URL, 
    ignoreHTTPSErrors: true,
    trace: 'on-first-retry',
  },
  
  /* Configure for major browsers */
  projects: [
    {
      name: 'setup',
      testDir: './',
      testMatch: '**/.auth/setup.ts',
      /* Only changes the URL for this specific project */
      use: {
        baseURL: process.env.CONSOLE_URL,       
      },
    },

    // Update chromium project
    {
      name: 'chromium',
      dependencies: ['setup'],
      use: { 
        ...devices['Desktop Chrome'],
        storageState: '.auth/storageState.json',
        // project still has ignoreHTTPSErrors: true from above
      },
    },

    {
      name: 'firefox',
      use: { 
        ...devices['Desktop Firefox'],
      },
    },
  ],
});