package alfred

import (
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"
)

// RunTrigger calls the External Trigger name in this workflow with arg as its
// argument.
func (wf *Workflow) RunTrigger(name, arg string) error {
	opts := map[string]string{"inWorkflow": wf.Env.BundleID}
	if arg != "" {
		opts["withArgument"] = arg
	}
	jsName, err := json.Marshal(name)
	if err != nil {
		return err
	}
	jsOpts, err := json.Marshal(opts)
	if err != nil {
		return err
	}
	script := fmt.Sprintf(`Application("com.runningwithcrayons.Alfred").runTrigger(%s, %s);`, jsName, jsOpts)
	out, err := exec.Command("/usr/bin/osascript", "-l", "JavaScript", "-e", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("run trigger %s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ReportError fires the "error" External Trigger with err's message. It's
// for background jobs and Run Scripts, whose stdout nothing reads.
func (wf *Workflow) ReportError(err error) {
	log.Printf("error: %v", err)
	if terr := wf.RunTrigger("error", err.Error()); terr != nil {
		log.Printf("report error: %v", terr)
	}
}
