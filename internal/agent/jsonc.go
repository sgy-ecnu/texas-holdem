package agent

import (
	"bytes"
	"encoding/json"
	"errors"
)

// stripJSONC is a lexical scanner, not a regexp replacement. Comment bytes
// become whitespace so adjacent tokens cannot accidentally concatenate.
func stripJSONC(src []byte) ([]byte, error) {
	out := make([]byte, 0, len(src))
	inString, escape := false, false
	for i := 0; i < len(src); i++ {
		c := src[i]
		if inString {
			out = append(out, c)
			if escape {
				escape = false
			} else if c == '\\' {
				escape = true
			} else if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			out = append(out, c)
			continue
		}
		if c == '/' && i+1 < len(src) {
			switch src[i+1] {
			case '/':
				out = append(out, ' ')
				i += 2
				for i < len(src) && src[i] != '\n' && src[i] != '\r' {
					i++
				}
				if i < len(src) {
					out = append(out, src[i])
				}
				continue
			case '*':
				out = append(out, ' ')
				i += 2
				for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
					i++
				}
				if i+1 >= len(src) {
					return nil, errors.New("CONFIG_INVALID")
				}
				i++
				continue
			}
		}
		out = append(out, c)
	}
	if inString {
		return nil, errors.New("CONFIG_INVALID")
	}
	// Remove a comma only when the next non-whitespace token closes a container.
	clean := make([]byte, 0, len(out))
	inString, escape = false, false
	for i, c := range out {
		if inString {
			clean = append(clean, c)
			if escape {
				escape = false
			} else if c == '\\' {
				escape = true
			} else if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
		}
		if c == ',' {
			prev := i - 1
			for prev >= 0 && isJSONSpace(out[prev]) {
				prev--
			}
			if prev < 0 || out[prev] == '{' || out[prev] == '[' || out[prev] == ',' || out[prev] == ':' {
				return nil, errors.New("CONFIG_INVALID")
			}
			j := i + 1
			for j < len(out) && isJSONSpace(out[j]) {
				j++
			}
			if j < len(out) && (out[j] == '}' || out[j] == ']') {
				continue
			}
		}
		clean = append(clean, c)
	}
	if !json.Valid(clean) {
		return nil, errors.New("CONFIG_INVALID")
	}
	return clean, nil
}

func isJSONSpace(b byte) bool { return b == ' ' || b == '\t' || b == '\r' || b == '\n' }

// rejectDuplicateKeys prevents ambiguous override/auth/action semantics.
func rejectDuplicateKeys(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var value func() error
	value = func() error {
		tok, err := d.Token()
		if err != nil {
			return errors.New("CONFIG_INVALID")
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			keys := make(map[string]bool)
			for d.More() {
				tok, err = d.Token()
				key, ok := tok.(string)
				if err != nil || !ok || keys[key] {
					return errors.New("CONFIG_INVALID")
				}
				keys[key] = true
				if err := value(); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err := value(); err != nil {
					return err
				}
			}
		default:
			return errors.New("CONFIG_INVALID")
		}
		_, err = d.Token()
		if err != nil {
			return errors.New("CONFIG_INVALID")
		}
		return nil
	}
	if !json.Valid(data) {
		return errors.New("CONFIG_INVALID")
	}
	return value()
}
