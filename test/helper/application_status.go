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
	err = cmd.Run()
	cmd.Stdout = &stdout
	if err != nil {
		return err
	}

	if strings.Trim(stdout.String(), " ") != "Healthy" {
		return fmt.Errorf(stdout.String())
	}

	return nil
}

func ApplicationSyncStatus(appname string, namespace string) error {
	var stdout bytes.Buffer
	ocPath, err := exec.LookPath("oc")
	if err != nil {
		return err
	}

	cmd := exec.Command(ocPath, "get", "application", "-n", namespace, "-o", "jsonpath='{.items[?(@.metame==\""+appname+"\")].status.sync.status}'")
	err = cmd.Run()
	cmd.Stdout = &stdout
	if err != nil {
		return err
	}

	if strings.Trim(stdout.String(), " ") != "Synced" {
		return fmt.Errorf(stdout.String())
	}

	return nil
}
