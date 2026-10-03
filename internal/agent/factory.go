package agent

import (
	"errors"
	"net/http"
	"sync"
	"time"
)

// Options 约束 agent 请求超时与输出 token 限额；零值回退到安全默认。
type Options struct {
	RequestTimeout  time.Duration
	MaxOutputTokens int
}

func (o Options) normalized() Options {
	if o.RequestTimeout <= 0 {
		o.RequestTimeout = defaultTimeout
	}
	if o.MaxOutputTokens <= 0 {
		o.MaxOutputTokens = defaultMaxTokens
	}
	return o
}

type modelFactory struct {
	mu     sync.RWMutex
	models map[string]modelConfig
	client *http.Client
	opts   Options
}

func (*modelFactory) String() string               { return "agent.Factory(redacted)" }
func (*modelFactory) GoString() string             { return "agent.Factory(redacted)" }
func (*modelFactory) MarshalJSON() ([]byte, error) { return []byte(`{}`), nil }

func NewFactory(catalog *Catalog) (Factory, error) {
	return NewFactoryWithOptions(catalog, Options{})
}

func NewFactoryWithOptions(catalog *Catalog, opts Options) (Factory, error) {
	if catalog == nil {
		return nil, errors.New("CONFIG_REQUIRED")
	}
	o := opts.normalized()
	f := &modelFactory{models: make(map[string]modelConfig), opts: o, client: &http.Client{
		Timeout: o.RequestTimeout,
		// Refuse all redirects, including same-host redirects, so credentials
		// can never be forwarded to an endpoint not configured by the admin.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
	for ref, config := range catalog.models {
		headers := make(map[string]string, len(config.headers))
		for k, v := range config.headers {
			headers[k] = v
		}
		config.headers = headers
		f.models[ref] = config
	}
	return f, nil
}

func (f *modelFactory) Models() []ModelDescriptor {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return catalogModels(&Catalog{models: f.models})
}

func (f *modelFactory) NewDecider(modelRef string) (Decider, error) {
	if modelRef == "mock" {
		return NewMockDecider(), nil
	}
	f.mu.RLock()
	config, exists := f.models[modelRef]
	f.mu.RUnlock()
	if !exists {
		return nil, errors.New("MODEL_NOT_FOUND")
	}
	if config.descriptor.Provider == "unsupported" {
		return nil, errors.New("MODEL_UNSUPPORTED")
	}
	if !config.ready {
		return nil, errors.New("MODEL_NOT_CONFIGURED")
	}
	return &liveDecider{config: config, client: f.client, timeout: f.opts.RequestTimeout, maxTokens: f.opts.MaxOutputTokens, setAvailability: func(availability Availability) {
		f.mu.Lock()
		defer f.mu.Unlock()
		current := f.models[modelRef]
		current.descriptor.Availability = availability
		f.models[modelRef] = current
	}}, nil
}
