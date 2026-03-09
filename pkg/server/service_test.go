package server

import "testing"

func TestHashLinkDomain(t *testing.T) {
	tests := []struct {
		domain string
		want   uint32
	}{
		// "default" is the OpenIPC standard domain
		// SHA1("default") = 7505d64a54e061b7acd54ccd58b49dc43500b635
		// First 3 bytes: 0x7505d6 = 7669206
		{"default", 7669206},
		{"", 7669206}, // Empty string should default to "default"

		// Other domains for regression testing
		{"my-drone", 13439057},
		{"test", 11094671},
		{"my-drone-1", 11916549},
	}

	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			got := HashLinkDomain(tt.domain)
			if got != tt.want {
				t.Errorf("HashLinkDomain(%q) = %d (0x%06x), want %d (0x%06x)",
					tt.domain, got, got, tt.want, tt.want)
			}
		})
	}
}

func TestHashLinkDomainCompatibility(t *testing.T) {
	// This test verifies compatibility with wfb-ng's hash_link_domain function
	// wfb-ng uses: int.from_bytes(hashlib.sha1(link_domain.encode('utf-8')).digest()[:3], 'big')
	//
	// The "default" domain is particularly important because it's the default
	// link_domain in OpenIPC firmware. ID 7669206 ensures out-of-the-box
	// compatibility with stock OpenIPC drones.

	linkID := HashLinkDomain("default")
	if linkID != 7669206 {
		t.Errorf("HashLinkDomain(\"default\") = %d, want 7669206 (OpenIPC default)", linkID)
		t.Error("This breaks compatibility with wfb-ng and OpenIPC!")
	}
}
