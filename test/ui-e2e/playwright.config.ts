import { defineConfig, devices } from '@playwright/test';

/**
 * Read environment variables from file.
 * https://github.com/motdotla/dotenv
 */

// top of playwright.config.ts
import dotenv from 'dotenv';
import path from 'path';
dotenv.config({ path: path.resolve(__dirname, '.env') });

/**
 * See https://playwright.dev/docs/test-configuration.
 */
export default defineConfig({
  testDir: './tests',
  /* Run tests in files in parallel */
  fullyParallel: true,
  /* Fail the build on CI if you accidentally left test.only in the source code. */
  forbidOnly: !!process.env.CI,
  /* Retry on CI only */
  retries: process.env.CI ? 2 : 0,
  /* Opt out of parallel tests on CI. */
  workers: process.env.CI ? 1 : undefined,
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
        baseURL: process.env.CONSOLE_URL,       },
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
        // storageState and dependencies here later if we want to run Firefox tests but for now just focus on Chromium
      },
    },
    // ... webkit etc ...
  ],

  /* Run your local dev server before starting the tests */
  // webServer: {
  //   command: 'npm run start',
  //   url: 'http://localhost:3000',
  //   reuseExistingServer: !process.env.CI,
  // },
});
