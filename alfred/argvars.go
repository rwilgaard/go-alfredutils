package alfred

import (
	"encoding/json"
	"io"
	"os"
)

// ArgVars is the output of a Run Script that passes an arg and variables to
// the next object.
type ArgVars struct {
	arg    []string
	argSet bool
	vars   map[string]string
}

func NewArgVars() *ArgVars { return &ArgVars{} }

func (a *ArgVars) Arg(args ...string) *ArgVars { a.arg, a.argSet = args, true; return a }

func (a *ArgVars) Var(name, value string) *ArgVars {
	if a.vars == nil {
		a.vars = map[string]string{}
	}
	a.vars[name] = value
	return a
}

// Send writes the JSON to stdout.
func (a *ArgVars) Send() error { return a.Write(os.Stdout) }

func (a *ArgVars) Write(w io.Writer) error { return json.NewEncoder(w).Encode(a) }

func (a *ArgVars) MarshalJSON() ([]byte, error) {
	type inner struct {
		Arg       any               `json:"arg,omitempty"`
		Variables map[string]string `json:"variables,omitempty"`
	}
	return json.Marshal(struct {
		Alfredworkflow inner `json:"alfredworkflow"`
	}{inner{argValue(a.arg, a.argSet), a.vars}})
}
