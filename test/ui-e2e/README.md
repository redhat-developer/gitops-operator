# OpenShift GitOps Operator - UI End-to-End Test Suite

This directory contains the Playwright-based UI End-to-End (E2E) automation suite for the OpenShift GitOps Operator. It validates core frontend workflows, console integration, Red Hat Single Sign-On (RHSSO) loops, and multi-version Argo CD compatibility across OpenShift clusters.

---

## Prerequisites

Before running the suite locally, ensure your machine has the following tools installed:

1. **Node.js** (v18 or higher)
2. **OpenShift CLI (oc)**: Must be configured in your system PATH.
3. **Browser Binaries**: Playwright requires its own specific browser engines to run tests reproducibly. These are installed automatically when you run the `npx playwright install` setup command.

### Installation

Navigate to this directory and install the Node modules along with the required Playwright browser binaries:

```bash
cd test/ui-e2e
npm install
npx playwright install chromium
```

---

## Environment Configuration

The test suite requires cluster administrative credentials to discover routes and handle authentication loops. You can configure these either via a local `.env` file or by exporting them directly into your terminal/CI environment pipeline.

### Quick Setup (Local Development)

Generate a local `.env` file in the root of this directory using the following block:

```bash
cat <<EOF> .env
export CLUSTER_USER="kubeadmin"
export CLUSTER_PASSWORD="<your_cluster_password>"
export OC_API_URL="<your_cluster_server_url>"
export IDP="kube:admin" # (Optional) Defaults to kube:admin

# Optional — private-repo.spec.ts (credentials in Bitwarden or cluster secret)
export PRIVATE_REPO_URL="<private_git_https_url>"
export PRIVATE_REPO_TOKEN="<access_token_or_pat>"
EOF
```

> **Security Warning:** The `.env` file is explicitly ignored by Git. Please don't commit credentials to the repository.

**Private Repository Test:** The test is **skipped** unless both `PRIVATE_REPO_URL` and `PRIVATE_REPO_TOKEN` are set. Credentials are stored in:
- **Bitwarden** (for local testing)
- **Cluster Secret** `ui-e2e-private-repo` (for CI/pipeline execution)

The username defaults to `x-access-token` for token-based authentication and does not need to be configured.

---

## Execution Commands

All executions are driven via the `./run-ui-tests.sh` wrapper script. This wrapper syncs your local oc CLI context to match your `.env` configuration, discovers the Console and Argo CD routes, and starts Playwright.

Run the suite on Chrome with `--project=chromium`. That project is the full set: the Argo CD specs and `tests/monitoring/`. A local `.env` in this directory is loaded for local runs. If it points at another cluster, move it aside before a run that should use a different login.

> **IMPORTANT: Sequential Execution (`--workers=1`)**
> The specs share the `openshift-gitops` namespace. **Always append `--workers=1` when running the suite** so they do not fight over it.

### Standard Test Execution

From `test/ui-e2e`:

| Target | Command |
| --- | --- |
| **All tests (headless)** | `./run-ui-tests.sh --project=chromium --workers=1` |
| **All tests (headed + trace)** | `./run-ui-tests.sh --project=chromium --workers=1 --headed --trace on` |
| **All tests (CI-style)** | `./run-ui-tests.sh --env=ci --project=chromium --workers=1` |
| **One Argo CD spec** | `./run-ui-tests.sh --project=chromium tests/resource-tree.spec.ts --workers=1 --headed` |
| **Observe harness only** | `./run-ui-tests.sh --project=chromium tests/monitoring/observe-harness.spec.ts --workers=1 --trace on --headed` |
| **Observe specs only** | `./run-ui-tests.sh --project=monitoring --workers=1` |

### Playwright Flags Reference

| Flag | Purpose |
| --- | --- |
| `--workers=1` | Forces tests to run sequentially (one at a time). Essential for preventing cluster resource race conditions when running multiple files. |
| `--headed` | Launches the visible Chromium browser UI. Excellent for local debugging. |
| `--trace on` | Records a granular execution trace (DOM snapshots, network calls, actions) for visual triage. |
| `--reporter=list` | Switches stdout to a clean line-by-line format, ideal for monitoring real-time execution steps. |
| `--env=<ci\|pipeline>` | Overrides the local setup to simulate automation. It forces headless execution, performs a clean `npm ci`, and installs required browser binaries dynamically. |

### Visual Debugging (Trace Viewer)

If a test fails during execution, Playwright records a full interactive timeline (DOM snapshots, network calls, console logs). 

When a test fails, the terminal output will provide an exact command to view the trace. Copy and paste that specific command:

```bash
# Example:
npx playwright show-trace test-results/create-application-chromium/trace.zip
```

---

## Monitoring (Observe)

`tests/monitoring/observe-harness.spec.ts` is included in the full `--project=chromium` run. It uses the same console login as the other specs and opens **Observe → Dashboards** and **Observe → Metrics**. Cluster monitoring needs to be enabled, with the GitOps operator installed.

Run that spec on its own:

```bash
cd test/ui-e2e
./run-ui-tests.sh --project=chromium tests/monitoring/observe-harness.spec.ts --workers=1 --trace on --headed
```

Headless:

```bash
./run-ui-tests.sh --project=chromium tests/monitoring/observe-harness.spec.ts --workers=1
./run-ui-tests.sh --env=ci --project=chromium tests/monitoring/observe-harness.spec.ts --workers=1
```

`--project=monitoring` runs every spec under `tests/monitoring/` and leaves the Argo CD specs out. A version matrix that already passes `--project=chromium` can target this file with `--test tests/monitoring/observe-harness.spec.ts`.

The harness checks that those pages open and that a dashboard can be chosen from the dropdown. It does not create an Argo CD instance.

Specs that ask for the `userDefinedArgoCD` fixture get a throwaway namespace `ui-e2e-mon-<timestamp>` and an `ArgoCD` named `argocd`. The fixture waits until the instance is `Available`, the namespace monitoring label is set, and the `ServiceMonitor` exists, then deletes the namespace. Give that spec a longer timeout (`test.setTimeout(13 * 60 * 1000)`). A new instance often takes several minutes.

To reuse an instance instead of creating one:

```bash
export MONITORING_ARGOCD_NAMESPACE="<namespace>"
export MONITORING_ARGOCD_NAME="<argocd-cr-name>"   # optional; openshift-gitops or argocd
```

When `MONITORING_ARGOCD_NAMESPACE` is set, the fixture leaves that namespace in place. `run-ui-tests.sh` still fills in `CONSOLE_URL` from the current `oc` login.

---

## Suite Architecture

```text
├── .auth/
│   └── setup.ts          # Orchestrates global OCP authentication & saves storageState.json
├── src/
│   ├── fixtures/
│   │   └── monitoring.ts # Observe pages + optional user-defined Argo CD
│   ├── pages/
│   │   ├── console/      # Observe → Dashboards and Metrics
│   │   ├── ApplicationsPage.ts
│   │   ├── ApplicationDetailsPage.ts
│   │   └── SettingsRepositoriesPage.ts
│   └── utils/
│       └── argocd-instance.ts
├── tests/
│   ├── monitoring/       # part of --project=chromium
│   │   └── observe-harness.spec.ts
│   ├── admin-login.spec.ts
│   ├── create-application.spec.ts
│   ├── resource-tree.spec.ts
│   ├── auto-sync-self-heal.spec.ts
│   └── private-repo.spec.ts
├── .env                  # Local runtime environment overrides (Git ignored)
└── run-ui-tests.sh       # Context-aware orchestrator & URL discovery engine
```

### Core Architecture Patterns

* **Global Authentication Reusability:** The `.auth/setup.ts` module runs first to execute the login sequence against the OpenShift cluster identity provider. It drops an authenticated session state cookie into `storageState.json`, allowing subsequent test specs to skip login actions entirely and save execution time.
* **Isolated SSO Specs:** Explicit UI authentication testing (such as `login.spec.ts`) bypasses global storage state configurations and clears active browser contexts intentionally to validate raw login screens and provider selections.
* **Cross-Version UI Abstraction:** Selectors inside the Page Object Models are written to withstand UI layout drift between consecutive OpenShift versions by prioritizing user-facing roles and text-based assertions over brittle CSS class trees.

---

## Troubleshooting

### Symptom: Playwright targets the wrong cluster version

* **Cause:** The wrapper script handles cross-cluster contexts dynamically. If your terminal environment variables don't match your local `~/.kube/config` cache, your terminal may fall back to cached sessions.
* **Resolution:** Ensure you either run `source .env` inside your terminal window to reset active shell contexts, or verify that the variables declared within your `.env` file match your active target system configuration.
