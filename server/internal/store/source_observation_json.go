package store

import (
	"bytes"
	"encoding/json"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"io"
	"reflect"
)

// Persisted financial observations use exact Go snapshot field names. Reject
// aliases, duplicates, unknown structure and non-canonical numeric facts.
func decodeSourceObservation(raw []byte, out *sourceObservation) error {
	return decodeSourceSnapshot(raw, out)
}
func decodeSourceSnapshot(raw []byte, out any) error {
	if len(raw) > 1<<20 {
		return si.ErrInvariant
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if e := sourceFactNode(d, reflect.TypeOf(out).Elem(), 0); e != nil {
		return e
	}
	if _, e := d.Token(); e != io.EOF {
		return si.ErrInvariant
	}
	if json.Unmarshal(raw, out) != nil {
		return si.ErrInvariant
	}
	return nil
}
func sourceFactNode(d *json.Decoder, t reflect.Type, depth int) error {
	if depth > 32 {
		return si.ErrInvariant
	}
	tok, e := d.Token()
	if e != nil {
		return si.ErrInvariant
	}
	pointer := t.Kind() == reflect.Pointer
	if pointer {
		t = t.Elem()
	}
	if tok == nil {
		if pointer {
			return nil
		}
		return si.ErrInvariant
	}
	if delim, ok := tok.(json.Delim); ok {
		if delim != '{' || t.Kind() != reflect.Struct {
			return si.ErrInvariant
		}
		seen := map[string]bool{}
		for d.More() {
			k, e := d.Token()
			if e != nil {
				return si.ErrInvariant
			}
			name, ok := k.(string)
			if !ok || seen[name] {
				return si.ErrInvariant
			}
			seen[name] = true
			f, ok := t.FieldByName(name)
			if !ok || f.PkgPath != "" {
				return si.ErrInvariant
			}
			if e = sourceFactNode(d, f.Type, depth+1); e != nil {
				return e
			}
		}
		end, e := d.Token()
		if e != nil || end != json.Delim('}') {
			return si.ErrInvariant
		}
		return nil
	}
	switch t.Kind() {
	case reflect.String:
		if _, ok := tok.(string); !ok {
			return si.ErrInvariant
		}
	case reflect.Bool:
		if _, ok := tok.(bool); !ok {
			return si.ErrInvariant
		}
	case reflect.Int64:
		n, ok := tok.(json.Number)
		if !ok {
			return si.ErrInvariant
		}
		if _, e := si.ParseMinor([]byte(n.String())); e != nil {
			return si.ErrInvariant
		}
	default:
		return si.ErrInvariant
	}
	return nil
}
