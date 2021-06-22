package helper

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// ProjectExists return true if the AppProject exists in the namespace,
// false otherwise (with an error, if available).
func ProjectExists(projectName string, namespace string) (bool, error) {
	var stdout, stderr bytes.Buffer
	ocPath, err := exec.LookPath("oc")
	if err != nil {
		return false, err
	}

	cmd := exec.Command(ocPath, "get", "appproject/"+projectName, "-n", namespace, "-o", "yaml")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("oc command failed: %s%s", stdout.String(), stderr.String())
	}

	return true, nil
}

// ApplicationHealthStatus returns an error if the application is not 'Healthy'
func ApplicationHealthStatus(appname string, namespace string) error {
	var stdout, stderr bytes.Buffer
	ocPath, err := exec.LookPath("oc")
	if err != nil {
		return err
	}

	cmd := exec.Command(ocPath, "get", "application/"+appname, "-n", namespace, "-o", "jsonpath='{.status.health.status}'")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("oc command failed: %s%s", stdout.String(), stderr.String())
	}

	if output := strings.TrimSpace(stdout.String()); output != "'Healthy'" {
		return fmt.Errorf("application '%s' health is %s", appname, output)
	}

	return nil
}

// ApplicationSyncStatus returns an error if the application is not 'Synced'
func ApplicationSyncStatus(appname string, namespace string) error {
	var stdout, stderr bytes.Buffer
	ocPath, err := exec.LookPath("oc")
	if err != nil {
		return err
	}

	cmd := exec.Command(ocPath, "get", "application/"+appname, "-n", namespace, "-o", "jsonpath='{.status.sync.status}'")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("oc command failed: %s%s", stdout.String(), stderr.String())
	}

	if output := strings.TrimSpace(stdout.String()); output != "'Synced'" {
		return fmt.Errorf("application '%s' status is %s", appname, output)
	}

	return nil
}
