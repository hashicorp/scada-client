package client

import (
	"testing"
	"time"
)

func testProviderConfig() *ProviderConfig {
	return &ProviderConfig{
		Endpoint: "127.0.0.1:65500", // Blackhole
		Service: &ProviderService{
			Service:        "test",
			ServiceVersion: "v1.0",
			ResourceType:   "test",
		},
		ResourceGroup: "hashicorp/test",
		Token:         "abcdefg",
	}
}

func TestValidate(t *testing.T) {
	type tcase struct {
		valid bool
		inp   *ProviderConfig
	}
	tcases := []*tcase{
		&tcase{
			false,
			&ProviderConfig{},
		},
		&tcase{
			false,
			&ProviderConfig{
				Service: &ProviderService{},
			},
		},
		&tcase{
			false,
			&ProviderConfig{
				Service: &ProviderService{
					Service: "foo",
				},
			},
		},
		&tcase{
			false,
			&ProviderConfig{
				Service: &ProviderService{
					Service:        "foo",
					ServiceVersion: "v1.0",
				},
			},
		},
		&tcase{
			false,
			&ProviderConfig{
				Service: &ProviderService{
					Service:        "foo",
					ServiceVersion: "v1.0",
					ResourceType:   "foo",
				},
			},
		},
		&tcase{
			false,
			&ProviderConfig{
				Service: &ProviderService{
					Service:        "foo",
					ServiceVersion: "v1.0",
					ResourceType:   "foo",
				},
				ResourceGroup: "hashicorp/test",
			},
		},
		&tcase{
			true,
			&ProviderConfig{
				Service: &ProviderService{
					Service:        "foo",
					ServiceVersion: "v1.0",
					ResourceType:   "foo",
				},
				ResourceGroup: "hashicorp/test",
				Token:         "abcdefg",
			},
		},
		&tcase{
			false,
			&ProviderConfig{
				Service: &ProviderService{
					Service:        "foo",
					ServiceVersion: "v1.0",
					ResourceType:   "foo",
					Capabilities: map[string]int{
						"http": 1,
					},
				},
				ResourceGroup: "hashicorp/test",
				Token:         "abcdefg",
			},
		},
		&tcase{
			true,
			&ProviderConfig{
				Service: &ProviderService{
					Service:        "foo",
					ServiceVersion: "v1.0",
					ResourceType:   "foo",
					Capabilities: map[string]int{
						"http": 1,
					},
				},
				ResourceGroup: "hashicorp/test",
				Token:         "abcdefg",
				Handlers: map[string]CapabilityProvider{
					"http": nil,
				},
			},
		},
	}
	for idx, tc := range tcases {
		err := validateConfig(tc.inp)
		if (err == nil) != tc.valid {
			t.Fatalf("%d err: %v", idx, err)
		}
	}
}

func TestProvider_StartStop(t *testing.T) {
	p, err := NewProvider(testProviderConfig())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if p.IsShutdown() {
		t.Fatalf("bad")
	}

	p.Shutdown()

	if !p.IsShutdown() {
		t.Fatalf("bad")
	}
}

func TestProvider_backoff(t *testing.T) {
	p, err := NewProvider(testProviderConfig())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	defer p.Shutdown()

	if b := p.backoffDuration(); b != DefaultBackoff {
		t.Fatalf("bad: %v", b)
	}

	// Set a new minimum
	p.backoffLock.Lock()
	p.backoff = 60 * time.Second
	p.backoffLock.Unlock()

	if b := p.backoffDuration(); b != 60*time.Second {
		t.Fatalf("bad: %v", b)
	}

	// Set no retry
	p.backoffLock.Lock()
	p.noRetry = true
	p.backoffLock.Unlock()

	if b := p.backoffDuration(); b != 0 {
		t.Fatalf("bad: %v", b)
	}
}
