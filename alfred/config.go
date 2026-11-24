package alfred

import (
	"fmt"
	"reflect"
	"strconv"
	"time"
)

var durationType = reflect.TypeFor[time.Duration]()

// Config fills the fields of the struct cfg points to from workflow variables,
// named by each field's `env:"name"` tag. A missing or empty variable leaves
// the field as it is, so defaults can be set before the call. Supported field
// types are string, bool, ints, floats and time.Duration, which takes a Go
// duration ("90s") or whole seconds ("90").
func (wf *Workflow) Config(cfg any) error {
	v := reflect.ValueOf(cfg)
	if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("config: want a pointer to a struct, got %T", cfg)
	}
	v = v.Elem()
	t := v.Type()
	for i := range t.NumField() {
		name := t.Field(i).Tag.Get("env")
		if name == "" || name == "-" {
			continue
		}
		raw, ok := wf.lookup(name)
		if !ok || raw == "" {
			continue
		}
		if err := setField(v.Field(i), raw); err != nil {
			return fmt.Errorf("config: %s=%q: %w", name, raw, err)
		}
	}
	return nil
}

func setField(f reflect.Value, raw string) error {
	if f.Type() == durationType {
		d, err := time.ParseDuration(raw)
		if err != nil {
			secs, ierr := strconv.ParseInt(raw, 10, 64)
			if ierr != nil {
				return err
			}
			d = time.Duration(secs) * time.Second
		}
		f.SetInt(int64(d))
		return nil
	}

	switch f.Kind() {
	case reflect.String:
		f.SetString(raw)
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return err
		}
		f.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(raw, 10, f.Type().Bits())
		if err != nil {
			return err
		}
		f.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(raw, 10, f.Type().Bits())
		if err != nil {
			return err
		}
		f.SetUint(n)
	case reflect.Float32, reflect.Float64:
		n, err := strconv.ParseFloat(raw, f.Type().Bits())
		if err != nil {
			return err
		}
		f.SetFloat(n)
	default:
		return fmt.Errorf("unsupported field type %s", f.Type())
	}
	return nil
}
