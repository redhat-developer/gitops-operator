package helper

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

func ApplicationHealthStatus(appname string, namespace string) error {
	var stdout bytes.Buffer
	ocPath, err := exec.LookPath("oc")
	if err != nil {
		return err
	}

	cmd := exec.Command(ocPath, "get", "application", "-n", namespace, "-o", "jsonpath='{.items[?(@.metadata.name==\""+appname+"\")].status.health.status}'")
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return err
	}

	output := strings.TrimSpace(stdout.String())

	if output != "'Healthy'" {
		return fmt.Errorf("application '%s' health is %v", appname, output)
	}

	return nil
}

func ApplicationSyncStatus(appname string, namespace string) error {
	var stdout bytes.Buffer
	ocPath, err := exec.LookPath("oc")
	if err != nil {
		return err
	}

	cmd := exec.Command(ocPath, "get", "application", "-n", namespace, "-o", "jsonpath='{.items[?(@.metadata.name==\""+appname+"\")].status.sync.status}'")
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return err
	}

	output := strings.TrimSpace(stdout.String())

	if output != "'Synced'" {
		return fmt.Errorf("application '%s' status is %s", appname, output)
	}

	return nil
}
