import { execSync } from 'child_process';

async function globalSetup() {
  console.log(' * Running pre-flight cleanup...');

  try {
    console.log('   -> Sweeping ghost applications...');
    //no hangs on dead controllers
    execSync('oc delete applications.argoproj.io --all -n openshift-gitops --wait=false', { stdio: 'ignore' });

    console.log('   -> Sweeping orphaned Spring Petclinic resources...');
    //no hangs on dead controllers
    execSync('oc delete all -l app=spring-petclinic -n openshift-gitops --wait=false', { stdio: 'ignore' });

    console.log('* Cluster sanitized. Starting test suite.');
    } catch (error) {
    console.error('Pre-flight cleanup failed. Check your cluster connection.', error);
    throw error;
    }
}

export default globalSetup;