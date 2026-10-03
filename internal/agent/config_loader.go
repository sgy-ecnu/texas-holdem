package agent

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const maxConfigBytes = 4 << 20

type Catalog struct{ models map[string]modelConfig }

type modelConfig struct {
	descriptor ModelDescriptor
	modelID    string
	endpoint   string
	apiKey     string
	headers    map[string]string
	ready      bool
}

// Catalog never exposes its private source, endpoint, or credentials via formatting.
func (*Catalog) String() string               { return "agent.Catalog(redacted)" }
func (*Catalog) GoString() string             { return "agent.Catalog(redacted)" }
func (*Catalog) MarshalJSON() ([]byte, error) { return []byte(`{}`), nil }

// LoadCatalog reads only the requested file and, for opencode.json/jsonc,
// its exact sibling. JSONC wins regardless of which sibling was supplied.
func LoadCatalog(path string) (*Catalog, error) {
	if path == "" {
		return nil, errors.New("CONFIG_PATH_REQUIRED")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, errors.New("CONFIG_PATH_INVALID")
	}
	files := []string{path}
	if name := filepath.Base(path); name == "opencode.json" || name == "opencode.jsonc" {
		files = []string{filepath.Join(filepath.Dir(path), "opencode.json"), filepath.Join(filepath.Dir(path), "opencode.jsonc")}
	}
	merged := make(map[string]any)
	found := false
	for _, file := range files {
		data, err := readLimited(file, maxConfigBytes)
		if err != nil {
			if os.IsNotExist(err) && file != path {
				continue
			}
			return nil, errors.New("CONFIG_READ_FAILED")
		}
		found = true
		if filepath.Ext(file) == ".jsonc" {
			data, err = stripJSONC(data)
			if err != nil {
				return nil, errors.New("CONFIG_INVALID")
			}
		}
		if rejectDuplicateKeys(data) != nil {
			return nil, errors.New("CONFIG_INVALID")
		}
		var root map[string]any
		if json.Unmarshal(data, &root) != nil || root == nil {
			return nil, errors.New("CONFIG_INVALID")
		}
		mergeObjects(merged, root)
	}
	if !found {
		return nil, errors.New("CONFIG_READ_FAILED")
	}
	c := &Catalog{models: make(map[string]modelConfig)}
	providers, ok := merged["provider"].(map[string]any)
	if !ok {
		if _, exists := merged["provider"]; exists {
			return nil, errors.New("CONFIG_INVALID")
		}
		return c, nil
	}
	for id, raw := range providers {
		if !safeID(id, false) {
			return nil, errors.New("CONFIG_INVALID")
		}
		p, ok := raw.(map[string]any)
		if !ok {
			return nil, errors.New("CONFIG_INVALID")
		}
		kind, err := providerKind(p)
		if err != nil {
			return nil, err
		}
		options := map[string]any{}
		if v, exists := p["options"]; exists {
			options, ok = v.(map[string]any)
			if !ok {
				return nil, errors.New("CONFIG_INVALID")
			}
		}
		base, err := stringOption(options, "baseURL")
		if err != nil {
			return nil, err
		}
		key, err := stringOption(options, "apiKey")
		if err != nil {
			return nil, err
		}
		base, baseReady, err := resolveValue(base, filepath.Dir(path))
		if err != nil {
			return nil, err
		}
		key, keyReady, err := resolveValue(key, filepath.Dir(path))
		if err != nil {
			return nil, err
		}
		if strings.ContainsAny(key, "\r\n") {
			return nil, errors.New("CONFIG_CREDENTIAL_INVALID")
		}
		if base == "" && baseReady {
			npm, _ := stringOption(p, "npm")
			if npm == "" {
				npm, _ = stringOption(p, "package")
			}
			switch npm {
			case "@ai-sdk/openai":
				base = "https://api.openai.com/v1"
			case "@ai-sdk/anthropic":
				base = "https://api.anthropic.com/v1"
			}
		}
		if base != "" && !validEndpoint(base) {
			return nil, errors.New("CONFIG_ENDPOINT_INVALID")
		}
		headers := make(map[string]string)
		headersReady := true
		if rawHeaders, exists := options["headers"]; exists {
			h, ok := rawHeaders.(map[string]any)
			if !ok {
				return nil, errors.New("CONFIG_INVALID")
			}
			for name, rawValue := range h {
				v, ok := rawValue.(string)
				if !ok || !validHeaderName(name) {
					return nil, errors.New("CONFIG_HEADER_INVALID")
				}
				v, ready, err := resolveValue(v, filepath.Dir(path))
				if err != nil {
					return nil, err
				}
				if strings.ContainsAny(v, "\r\n") {
					return nil, errors.New("CONFIG_HEADER_INVALID")
				}
				canonical := http.CanonicalHeaderKey(name)
				if _, exists := headers[canonical]; exists {
					return nil, errors.New("CONFIG_HEADER_INVALID")
				}
				headers[canonical] = v
				headersReady = headersReady && ready
			}
		}
		models, ok := p["models"].(map[string]any)
		if !ok {
			if _, exists := p["models"]; exists {
				return nil, errors.New("CONFIG_INVALID")
			}
			continue
		}
		for model, rawModel := range models {
			if !safeID(model, true) {
				return nil, errors.New("CONFIG_INVALID")
			}
			m, ok := rawModel.(map[string]any)
			if !ok {
				return nil, errors.New("CONFIG_INVALID")
			}
			modelID := model
			if v, exists := m["id"]; exists {
				modelID, ok = v.(string)
				if !ok || !safeID(modelID, true) {
					return nil, errors.New("CONFIG_INVALID")
				}
			}
			ref := id + "/" + model
			ready := baseReady && keyReady && headersReady && base != "" && key != ""
			availability := Availability("configured")
			if !ready {
				availability = "unverified"
			}
			if kind == "unsupported" {
				availability = "unsupported"
				ready = false
			}
			// Deliberately use the model ID as label, not arbitrary config text.
			c.models[ref] = modelConfig{descriptor: ModelDescriptor{Ref: ref, Label: model, Provider: kind, Availability: availability}, modelID: modelID, endpoint: base, apiKey: key, headers: headers, ready: ready}
		}
	}
	return c, nil
}

func readLimited(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(b)) > limit {
		return nil, errors.New("CONFIG_READ_FAILED")
	}
	return b, nil
}

func mergeObjects(dst, src map[string]any) {
	for k, v := range src {
		a, aok := dst[k].(map[string]any)
		b, bok := v.(map[string]any)
		if aok && bok {
			mergeObjects(a, b)
		} else {
			dst[k] = v
		}
	}
}

func providerKind(p map[string]any) (ProviderKind, error) {
	npm, err := stringOption(p, "npm")
	if err != nil {
		return "", err
	}
	pkg, err := stringOption(p, "package")
	if err != nil {
		return "", err
	}
	if npm != "" && pkg != "" && npm != pkg {
		return "unsupported", nil
	}
	if npm == "" {
		npm = pkg
	}
	switch npm {
	case "@ai-sdk/openai", "@ai-sdk/openai-compatible":
		return "openai_compatible", nil
	case "@ai-sdk/anthropic":
		return "anthropic", nil
	default:
		return "unsupported", nil
	}
}

func stringOption(m map[string]any, name string) (string, error) {
	v, exists := m[name]
	if !exists {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", errors.New("CONFIG_INVALID")
	}
	return s, nil
}

func resolveValue(s, dir string) (string, bool, error) {
	if strings.HasPrefix(s, "{env:") && strings.HasSuffix(s, "}") {
		name := s[5 : len(s)-1]
		if !envName.MatchString(name) {
			return "", false, errors.New("CONFIG_REFERENCE_INVALID")
		}
		v, ok := os.LookupEnv(name)
		return v, ok && v != "", nil
	}
	if strings.HasPrefix(s, "{file:") && strings.HasSuffix(s, "}") {
		path := s[6 : len(s)-1]
		if path == "" {
			return "", false, errors.New("CONFIG_REFERENCE_INVALID")
		}
		if strings.HasPrefix(path, "~/") {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", false, errors.New("CONFIG_CREDENTIAL_READ_FAILED")
			}
			path = filepath.Join(home, path[2:])
		} else if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		b, err := readLimited(path, 64<<10)
		if err != nil {
			return "", false, errors.New("CONFIG_CREDENTIAL_READ_FAILED")
		}
		v := strings.TrimSpace(string(b))
		return v, v != "", nil
	}
	if strings.HasPrefix(s, "{env:") || strings.HasPrefix(s, "{file:") {
		return "", false, errors.New("CONFIG_REFERENCE_INVALID")
	}
	return s, true, nil
}

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var providerID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`)
var modelName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:/-]{0,149}$`)
var headerName = regexp.MustCompile("^[!#$%&'*+.^_`|~A-Za-z0-9-]+$")

func safeID(s string, model bool) bool {
	if model {
		return modelName.MatchString(s)
	}
	return providerID.MatchString(s)
}

func validEndpoint(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}

func validHeaderName(s string) bool {
	if !headerName.MatchString(s) {
		return false
	}
	switch strings.ToLower(s) {
	case "host", "content-length", "transfer-encoding", "connection", "content-type", "authorization", "x-api-key", "anthropic-version":
		return false
	}
	return true
}

func catalogModels(c *Catalog) []ModelDescriptor {
	models := []ModelDescriptor{{Ref: "mock", Label: "Mock", Provider: "mock", Availability: "mock"}}
	if c != nil {
		for _, m := range c.models {
			models = append(models, m.descriptor)
		}
	}
	sort.Slice(models[1:], func(i, j int) bool { return models[i+1].Ref < models[j+1].Ref })
	return models
}
