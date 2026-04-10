package ai

import "sync"

// StreamFunc is a provider's implementation of the raw streaming API.
type StreamFunc func(model Model, ctx Context, opts *StreamOptions) *AssistantMessageEventStream

// SimpleStreamFunc is a provider's implementation of the simple (reasoning-
// aware) streaming API.
type SimpleStreamFunc func(model Model, ctx Context, opts *SimpleStreamOptions) *AssistantMessageEventStream

// ApiProvider bundles a streaming implementation with the API it speaks.
type ApiProvider struct {
	Api          Api
	Stream       StreamFunc
	StreamSimple SimpleStreamFunc
}

var (
	registryMu sync.RWMutex
	providers  = make(map[Api]*ApiProvider)
)

// RegisterProvider publishes a provider implementation under its Api. Later
// registrations for the same Api overwrite earlier ones. Safe to call from
// init() or at runtime.
func RegisterProvider(p *ApiProvider) {
	if p == nil {
		return
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	providers[p.Api] = p
}

// GetProvider looks up a provider by Api. The boolean result reports whether
// a provider was registered.
func GetProvider(api Api) (*ApiProvider, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	p, ok := providers[api]
	return p, ok
}
