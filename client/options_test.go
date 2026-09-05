package client

import (
	"net/http"
	"testing"
	"time"
)

func TestOptionsCompose(t *testing.T) {
	tr := &http.Transport{}
	custom := &http.Client{Transport: tr, Timeout: time.Minute}

	for name, opts := range map[string][]Option{
		"client-then-timeout": {WithHTTPClient(custom), WithTimeout(10 * time.Second)},
		"timeout-then-client": {WithTimeout(10 * time.Second), WithHTTPClient(custom)},
	} {
		d := newDevice("127.0.0.1:11111", "camera", 0, opts...)
		if d.http.Transport != tr {
			t.Errorf("%s: custom transport was discarded", name)
		}
		if d.http.Timeout != 10*time.Second {
			t.Errorf("%s: envelope timeout = %v, want 10s", name, d.http.Timeout)
		}
		if d.imageHTTP.Transport != tr {
			t.Errorf("%s: image client lost the custom transport", name)
		}
	}
}

func TestDefaultClients(t *testing.T) {
	d := newDevice("127.0.0.1:11111", "camera", 0)
	if d.http.Timeout != defaultTimeout {
		t.Errorf("envelope timeout = %v, want %v", d.http.Timeout, defaultTimeout)
	}
	if d.imageHTTP.Timeout != 0 {
		t.Errorf("image client timeout = %v, want 0 (unbounded body transfer)", d.imageHTTP.Timeout)
	}
	tr, ok := d.imageHTTP.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("image transport is %T, want *http.Transport", d.imageHTTP.Transport)
	}
	if tr.ResponseHeaderTimeout != defaultTimeout {
		t.Errorf("image ResponseHeaderTimeout = %v, want %v", tr.ResponseHeaderTimeout, defaultTimeout)
	}
}
