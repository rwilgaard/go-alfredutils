// Package plist reads the property lists Alfred writes: info.plist and
// prefs.plist.
package plist

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
)

const plutil = "/usr/bin/plutil"

// Read decodes the property list at path into a map, returning the file's
// raw bytes too. A binary plist is converted with plutil first.
func Read(path string) (map[string]any, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	src := data
	if bytes.HasPrefix(data, []byte("bplist")) {
		if src, err = exec.Command(plutil, "-convert", "xml1", "-o", "-", path).Output(); err != nil {
			return nil, nil, fmt.Errorf("convert %s: %w", path, err)
		}
	}
	v, err := Decode(src)
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, nil, fmt.Errorf("parse %s: top-level value is not a dict", path)
	}
	return m, data, nil
}

// Decode decodes an XML property list. Dicts become map[string]any,
// arrays []any, booleans bool, and every other value its text.
func Decode(data []byte) (any, error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := d.Token()
		if err != nil {
			return nil, fmt.Errorf("no plist value: %w", err)
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local != "plist" {
			return decodeValue(d, se)
		}
	}
}

func decodeValue(d *xml.Decoder, se xml.StartElement) (any, error) {
	switch se.Name.Local {
	case "dict":
		m := map[string]any{}
		key, haveKey := "", false
		for {
			tok, err := d.Token()
			if err != nil {
				return nil, err
			}
			switch t := tok.(type) {
			case xml.StartElement:
				if t.Name.Local == "key" {
					if err := d.DecodeElement(&key, &t); err != nil {
						return nil, err
					}
					haveKey = true
					continue
				}
				if !haveKey {
					return nil, fmt.Errorf("<%s> in dict without a key", t.Name.Local)
				}
				v, err := decodeValue(d, t)
				if err != nil {
					return nil, err
				}
				m[key] = v
				key, haveKey = "", false
			case xml.EndElement:
				return m, nil
			}
		}
	case "array":
		a := []any{}
		for {
			tok, err := d.Token()
			if err != nil {
				return nil, err
			}
			switch t := tok.(type) {
			case xml.StartElement:
				v, err := decodeValue(d, t)
				if err != nil {
					return nil, err
				}
				a = append(a, v)
			case xml.EndElement:
				return a, nil
			}
		}
	case "true", "false":
		if err := d.Skip(); err != nil {
			return nil, err
		}
		return se.Name.Local == "true", nil
	default:
		var s string
		if err := d.DecodeElement(&s, &se); err != nil {
			return nil, err
		}
		return s, nil
	}
}
