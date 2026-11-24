// Package ui builds the JSON actions that an Alfred UI (Alfred 6)
// script action writes to stdout.
// See https://www.alfredapp.com/help/alfredui/actions/json/
package ui

import (
	"encoding/json"
	"io"
	"os"
)

// Colours used for status messages.
const (
	ColourError   = "systemRedColor"
	ColourInfo    = "secondaryLabelColor"
	ColourSuccess = "systemGreenColor"
)

// Props maps component property names to values.
type Props map[string]any

// Update changes properties on existing components.
type Update struct {
	IDs      map[string]Props `json:"ids"`
	Relayout bool             `json:"relayout,omitempty"`
}

// Finish closes the UI and passes args and variables to the next workflow object.
type Finish struct {
	Args      any               `json:"args,omitempty"`
	Variables map[string]string `json:"variables,omitempty"`
}

// Sheet shows (Root set) or closes (Close set) a modal sheet.
type Sheet struct {
	ID            string `json:"id,omitempty"`
	Close         string `json:"close,omitempty"`
	CloseOnEscape *bool  `json:"closeonescape,omitempty"`
	Root          any    `json:"root,omitempty"`
}

// Pulse reconfigures the UI's pulse. Interval 0 disables it.
type Pulse struct {
	Interval float64 `json:"interval"`
}

// Actions is the top-level JSON object.
type Actions struct {
	Variables map[string]string `json:"variables,omitempty"`
	Update    *Update           `json:"update,omitempty"`
	Sheet     *Sheet            `json:"sheet,omitempty"`
	Pulse     *Pulse            `json:"pulse,omitempty"`
	Finish    *Finish           `json:"finish,omitempty"`
}

// Emit writes actions as JSON to stdout.
func Emit(a Actions) error { return Write(os.Stdout, a) }

// Write writes actions as JSON to w.
func Write(w io.Writer, a Actions) error { return json.NewEncoder(w).Encode(a) }

// NewUpdate returns an empty update action.
func NewUpdate() Actions { return Actions{Update: &Update{IDs: map[string]Props{}}} }

// Set sets properties on the component with the given id.
func (a Actions) Set(id string, p Props) Actions {
	if a.Update == nil {
		a.Update = &Update{IDs: map[string]Props{}}
	}
	if cur, ok := a.Update.IDs[id]; ok {
		for k, v := range p {
			cur[k] = v
		}
		return a
	}
	a.Update.IDs[id] = p
	return a
}

// Status sets the text and colour of a label.
func (a Actions) Status(labelID, msg, colour string) Actions {
	return a.Set(labelID, Props{"text": msg, "colour": colour})
}

// Enable enables or disables components.
func (a Actions) Enable(enabled bool, ids ...string) Actions {
	for _, id := range ids {
		a = a.Set(id, Props{"enabled": enabled})
	}
	return a
}

// SetVar sets a UI variable.
func (a Actions) SetVar(k, v string) Actions {
	if a.Variables == nil {
		a.Variables = map[string]string{}
	}
	a.Variables[k] = v
	return a
}

// WithSheet adds a sheet action.
func (a Actions) WithSheet(sh *Sheet) Actions {
	a.Sheet = sh
	return a
}

// Focus focuses a component.
func (a Actions) Focus(id string) Actions {
	return a.Set(id, Props{"focus": true})
}
