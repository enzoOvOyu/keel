package platform

import (
	"strings"
	"testing"
)

func TestParseLegacyBOMCSVFold(t *testing.T) {
	csv := "\xef\xbb\xbfSN,hostname,model_name,MAC,port_name,rail\nSN-1,node-1,server-v1,aa:bb:cc:dd:ee:01,p1,0\nSN-1,node-1,server-v1,aa:bb:cc:dd:ee:02,p2,1\n"
	got, err := ParseLegacyBOMCSV([]byte(csv))
	if err != nil || got.Rows != 2 || len(got.Input.Machines) != 1 || len(got.Input.Machines[0].Ports) != 2 {
		t.Fatalf("fold failed: %+v, %v", got, err)
	}
	for _, bad := range []string{
		strings.Replace(csv, "node-1,server-v1,aa:bb:cc:dd:ee:02", "node-2,server-v1,aa:bb:cc:dd:ee:02", 1),
		strings.Replace(csv, "aa:bb:cc:dd:ee:02", "aa:bb:cc:dd:ee:01", 1),
		strings.Replace(csv, "p2,1", "p1,1", 1),
	} {
		if _, err := ParseLegacyBOMCSV([]byte(bad)); err == nil {
			t.Fatalf("accepted conflicting rows: %q", bad)
		}
	}
}
