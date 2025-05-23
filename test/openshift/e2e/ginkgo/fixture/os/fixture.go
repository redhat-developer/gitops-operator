package os

import (
	"os/exec"

	. "github.com/onsi/ginkgo/v2"
)

func ExecCommand(cmdArgs ...string) (string, error) {
	return ExecCommandWithOutputParam(true, cmdArgs...)
}

// You probably want to use ExecCommand, unless you need to supress the output of sensitive data (for example, openssl CLI output)
func ExecCommandWithOutputParam(printOutput bool, cmdArgs ...string) (string, error) {
	GinkgoWriter.Println("executing command:", cmdArgs)

	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)

	outputBytes, err := cmd.CombinedOutput()

	var output string
	if outputBytes != nil {
		output = string(outputBytes)
	}

	if printOutput {
		GinkgoWriter.Println(output)
	}

	return output, err
}
