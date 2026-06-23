
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
cat <<EOF > .env
export CLUSTER_USER="kubeadmin"
export CLUSTER_PASSWORD="<your_cluster_password>"
export OC_API_URL="<your_cluster_server_url>"
export IDP="kube:admin" # (Optional) Defaults to kube:admin
EOF

```

> **Security Warning:** The `.env` file is explicitly ignored by Git. Please don't commit  credentials to the repository.

---

## Execution Commands

All executions are driven via the ./run-ui-tests.sh wrapper script. This wrapper automatically syncs your local oc CLI context to match your .env configuration, performs route discovery for the Console/Argo CD components, and initializes the Playwright runner.

### Standard Test Execution

| Target | Command |
| --- | --- |
| **Run All Tests (Local Headless)** | `./run-ui-tests.sh --project=chromium` |
| **Run All Tests (Local Headed + Trace)** | `./run-ui-tests.sh --project=chromium --headed --trace on` |
| **Run All Tests (Simulate CI)** | `./run-ui-tests.sh --env=ci --project=chromium` |
| **Run a Specific Spec File** | `./run-ui-tests.sh tests/resource-tree.spec.ts --project=chromium --headed` |

### Playwright Flags Reference

| Flag | Purpose |
| --- | --- |
| `--headed` | Launches the visible Chromium browser UI. Excellent for local debugging. |
| `--trace on` | Records a granular execution trace (DOM snapshots, network calls, actions) for visual triage. |
| `--reporter=list` | Switches stdout to a clean line-by-line format, ideal for monitoring real-time execution steps. |
| `--env=<ci|pipeline>` | Overrides the local setup to simulate automation. It forces headless execution, performs a clean `npm ci`, and installs required browser binaries dynamically. |

### Visual Debugging (Trace Viewer)

If a test fails during execution, Playwright records a full interactive timeline (DOM snapshots, network calls, console logs). 

When a test fails, the terminal output will provide an exact command to view the trace. Copy and paste that specific command:

```bash
# Example:
npx playwright show-trace test-results/create-application-chromium/trace.zip

```

---

## Suite Architecture

```text
├── .auth/
│   └── setup.ts          # Orchestrates global OCP authentication & saves storageState.json
├── src/
│   └── pages/            # Page Object Models (POM) isolating UI selectors from spec logic
│       └── ApplicationsPage.ts
├── tests/                # Test specs organized by feature epic
│   ├── admin-login.spec.ts
│   ├── create-application.spec.ts
│   └── resource-tree.spec.ts
├── .env                  # Local runtime environment overrides (Git ignored)
└── run-ui-tests.sh       # Context-aware orchestrator & URL discovery engine

```

### Core Architecture Patterns

* **Global Authentication Reusability:** The .auth/setup.ts module runs first to execute the login sequence against the OpenShift cluster identity provider. It drops an authenticated session state cookie into storageState.json, allowing subsequent test specs to skip login actions entirely and save execution time.
* **Isolated SSO Specs:** Explicit UI authentication testing (such as login.spec.ts) bypasses global storage state configurations and clears active browser contexts intentionally to validate raw login screens and provider selections.
* **Cross-Version UI Abstraction:** Selectors inside the Page Object Models are written to withstand UI layout drift between consecutive OpenShift versions by prioritizing user-facing roles and text-based assertions over brittle CSS class trees.

---

## Troubleshooting

### Symptom: Playwright targets the wrong cluster version

* **Cause:** The wrapper script handles cross-cluster contexts dynamically. If your terminal environment variables don't match your local ~/.kube/config cache, your terminal may fall back to cached sessions.
* **Resolution:** Ensure you either run `source .env` inside your terminal window to reset active shell contexts, or verify that the variables declared within your .env file match your active target system configuration. 

