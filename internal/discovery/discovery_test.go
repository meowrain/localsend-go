package discovery

import "testing"

func TestDiscovery(t *testing.T) {
	ips, err := pingScan()
	if err != nil {
		t.Log(err)
	}
	t.Log(ips)
}
