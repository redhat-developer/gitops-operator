import { expect, Locator, Page } from '@playwright/test';
import { dismissConsoleTour } from './tour';
import { consoleUrl } from './url';

const TIMEOUTS = {
  menu: 15000,
  load: 45000,
};

function escapeRegex(value: string) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

export class DashboardsPage {
  readonly page: Page;

  constructor(page: Page) {
    this.page = page;
  }

  private get dropdown() {
    return this.page.locator('[data-test="dashboard-dropdown"]');
  }

  //closed menus stick around in the dom
  private menuOptions(): Locator {
    return this.page.locator(
      [
        '[role="listbox"]:visible [role="option"]',
        '[role="menu"]:visible [role="menuitem"]',
        '.pf-c-select__menu:visible .pf-c-select__menu-item',
        '.pf-v5-c-menu:visible .pf-v5-c-menu__item',
        '.pf-v6-c-menu:visible .pf-v6-c-menu__item',
      ].join(', ')
    );
  }

  async open() {
    await this.page.goto(consoleUrl('/monitoring/dashboards'), {
      waitUntil: 'domcontentloaded',
      timeout: 60000,
    });
    await dismissConsoleTour(this.page);
    await expect(this.page.getByRole('heading', { name: /dashboards/i })).toBeVisible({
      timeout: TIMEOUTS.load,
    });
    await expect(this.dropdown).toBeVisible({ timeout: TIMEOUTS.load });
  }

  async listDashboards(): Promise<string[]> {
    await this.openMenu();
    const options = this.menuOptions();
    await expect(options.first()).toBeVisible({ timeout: TIMEOUTS.menu });

    const titles: string[] = [];
    const count = await options.count();
    for (let i = 0; i < count; i++) {
      const raw = await options.nth(i).innerText();
      const title = raw
        .split('\n')
        .map((line) => line.trim())
        .find(Boolean);
      if (title && !titles.includes(title)) titles.push(title);
    }

    return titles;
  }

  async selectDashboard(title: string) {
    await this.openMenu();

    const filter = this.dropdown.locator('input').first();
    if (await filter.isVisible()) {
      await filter.fill(title);
    }

    const match = this.menuOptions().filter({
      hasText: new RegExp(`^\\s*${escapeRegex(title)}`, 'i'),
    });
    await match.first().click();
  }

  async selectedDashboard(): Promise<string> {
    const input = this.dropdown.locator('input').first();
    if (await input.isVisible()) {
      const value = (await input.inputValue()).trim();
      if (value) return value;
    }

    const buttonText = (await this.dropdown.locator('button').first().innerText()).trim();
    return buttonText.split('\n')[0]?.trim() ?? buttonText;
  }

  private async openMenu() {
    await expect(this.dropdown).toBeVisible({ timeout: TIMEOUTS.load });
    //pf6 typeahead stays open, and the toggle click just mounts a new one
    if (await this.menuOptions().first().isVisible()) return;
    await this.dropdown.locator('button').first().click();
    await expect(this.menuOptions().first()).toBeVisible({ timeout: TIMEOUTS.menu });
  }
}
