// No trouble

package std

import "testing"

func TestParseMultiPort(t *testing.T) {
	tests := []struct {
		addr string
		want MultiPort
	}{
		{"example.com:29900-30100", MultiPort{Host: "example.com", MinPort: 29900, MaxPort: 30100}},
		{"192.0.2.1:29900", MultiPort{Host: "192.0.2.1", MinPort: 29900, MaxPort: 29900}},
		{":29900", MultiPort{MinPort: 29900, MaxPort: 29900}},
		{":1-65535", MultiPort{MinPort: 1, MaxPort: 65535}},
		{"[2001:db8::1]:65535", MultiPort{Host: "[2001:db8::1]", MinPort: 65535, MaxPort: 65535}},
		{"[::]:29900-30100", MultiPort{Host: "[::]", MinPort: 29900, MaxPort: 30100}},
		{"[fe80::1%eth0]:29900-30100", MultiPort{Host: "[fe80::1%eth0]", MinPort: 29900, MaxPort: 30100}},
	}

	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			got, err := ParseMultiPort(tt.addr)
			if err != nil {
				t.Fatalf("ParseMultiPort(%q): %v", tt.addr, err)
			}
			if *got != tt.want {
				t.Errorf("ParseMultiPort(%q) = %+v, want %+v", tt.addr, *got, tt.want)
			}
		})
	}
}

func TestParseMultiPortRejectsInvalidPorts(t *testing.T) {
	for _, addr := range []string{
		"example.com:",
		"example.com:0",
		"example.com:65536",
		"example.com:100000",
		"example.com:29900junk",
		"example.com:29900-",
		"example.com:29900-30100junk",
		"example.com:29900-30100-30200",
		"example.com:29900-65536",
		"example.com:30100-29900",
		"[2001:db8::1]:29900junk",
		"[2001:db8::1]:29900-",
	} {
		t.Run(addr, func(t *testing.T) {
			if got, err := ParseMultiPort(addr); err == nil {
				t.Errorf("ParseMultiPort(%q) = %+v, want an error", addr, got)
			}
		})
	}
}
