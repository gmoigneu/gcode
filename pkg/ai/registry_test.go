package ai

import (
	"sync"
	"testing"
)

func TestRegistryRegisterAndGet(t *testing.T) {
	resetRegistry(t)

	p := &ApiProvider{Api: "test-api"}
	RegisterProvider(p)

	got, ok := GetProvider("test-api")
	if !ok {
		t.Fatal("GetProvider returned !ok")
	}
	if got != p {
		t.Errorf("got %p, want %p", got, p)
	}
}

func TestRegistryGetMissing(t *testing.T) {
	resetRegistry(t)

	if _, ok := GetProvider("does-not-exist"); ok {
		t.Error("missing provider should return ok=false")
	}
}

func TestRegistryRegisterOverwrites(t *testing.T) {
	resetRegistry(t)

	first := &ApiProvider{Api: "over"}
	second := &ApiProvider{Api: "over"}
	RegisterProvider(first)
	RegisterProvider(second)

	got, _ := GetProvider("over")
	if got != second {
		t.Errorf("expected second registration to win")
	}
}

func TestRegistryConcurrent(t *testing.T) {
	resetRegistry(t)

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			RegisterProvider(&ApiProvider{Api: "concurrent"})
		}()
		go func() {
			defer wg.Done()
			_, _ = GetProvider("concurrent")
		}()
	}
	wg.Wait()

	if _, ok := GetProvider("concurrent"); !ok {
		t.Error("expected concurrent provider to be registered")
	}
}

// resetRegistry clears all registered providers so each test starts clean.
// Tests using this helper must not run in parallel.
func resetRegistry(t *testing.T) {
	t.Helper()
	registryMu.Lock()
	defer registryMu.Unlock()
	providers = make(map[Api]*ApiProvider)
}
