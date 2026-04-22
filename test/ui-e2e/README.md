# GitOps Operator - UI End-to-End Tests

This suite validates the OpenShift GitOps Operator UI, focusing on Argo CD and SSO integration.

##  Prerequisites
1. **Node.js** (v18+)
2. **OpenShift CLI (oc)**: Installed and in your PATH.
3. **Install Dependencies:** Navigate to this directory and install required packages:
   ```bash
   cd test/ui-e2e
   npm install
   npx playwright install chromium
   ```

##  Environment Variables
You must provide cluster credentials before running tests. You can either `export` these in your terminal (or pipeline), or create a `.env` file in the `test/ui-e2e` directory:

```text
# .env file example
CLUSTER_PASSWORD=your_openshift_admin_password
OC_API_URL=[https://api.cluster.com:6443](https://api.cluster.com:6443)
CLUSTER_USER=kubeadmin  # (Optional) Defaults to kubeadmin
IDP=kube:admin          # (Optional) Defaults to kube:admin
```

##  Execution Commands

All commands use the `./run-ui-tests.sh` wrapper which handles auth, OpenShift token generation, and URL discovery. **Ensure you are in the `test/ui-e2e` directory.**

**Run All Tests (Headless):**
```bash
./run-ui-tests.sh --project=chromium 
```

**Run All Tests (Headed + Trace):**
```bash
./run-ui-tests.sh --project=chromium --headed --reporter=list --trace on
```

**Run Single Test (Headed + Trace):**
```bash
./run-ui-tests.sh tests/login.spec.ts --project=chromium --headed --trace on
```

**View Trace Results:**
```bash
npx playwright show-trace test-results/**/*/trace.zip
```

** Helpful Flags Explained** 
* `--headed`: Runs tests in a visible browser. Without this, tests run in "headless" mode (invisible background).
* `--reporter=list`: Changes console output to a clean, line-by-line list so you can see exactly which test is running in real-time.
* `--trace on`: Captures a full "recording" (DOM snapshots, network, actions) of the test for debugging.

## Architecture 

**Global Setup:**
`.auth/setup.ts` logs into the OCP console to generate a reusable session (`storageState.json`). This prevents having to log in repeatedly for every test file.

**Spec Isolation:**
`login.spec.ts` explicitly clears session cookies to force a full SSO UI validation from a fresh state.

## Troubleshooting

* **"Invalid login or password" during automated login:** If you are testing against multiple clusters sequentially, your terminal's `oc` CLI might be holding onto a sticky session from an older cluster. Run `oc logout` before running the bash script to force a clean authentication.