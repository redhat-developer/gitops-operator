package helper

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// ApplicationHealthStatus returns an error if the application is not 'Healthy'
func ApplicationHealthStatus(appname string, namespace string) error {
	var stdout bytes.Buffer
	ocPath, err := exec.LookPath("oc")
	if err != nil {
		return err
	}

	cmd := exec.Command(ocPath, "get", "application/"+appname, "-n", namespace, "-o", "jsonpath='{.status.health.status}'")
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return err
	}

	if output := strings.TrimSpace(stdout.String()); output != "'Healthy'" {
		return fmt.Errorf("application '%s' health is %s", appname, output)
	}

	return nil
}

// ApplicationSyncStatus returns an error if the application is not 'Synced'
func ApplicationSyncStatus(appname string, namespace string) error {
	var stdout bytes.Buffer
	ocPath, err := exec.LookPath("oc")
	if err != nil {
		return err
	}

	cmd := exec.Command(ocPath, "get", "application/"+appname, "-n", namespace, "-o", "jsonpath='{.status.sync.status}'")
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return err
	}

	if output := strings.TrimSpace(stdout.String()); output != "'Synced'" {
		return fmt.Errorf("application '%s' status is %s", appname, output)
	}

	return nil
}
