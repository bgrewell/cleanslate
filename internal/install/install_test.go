package install

import "testing"

func TestStripPartitionSuffix(t *testing.T) {
	cases := map[string]string{
		"/dev/sda1":    "/dev/sda",
		"/dev/sdb12":   "/dev/sdb",
		"/dev/nvme0n1p1": "/dev/nvme0n1",
		"/dev/nvme0n1p13": "/dev/nvme0n1",
		"/dev/loop0p2": "/dev/loop0",
		"/dev/sda":     "",
		"":             "",
	}
	for in, want := range cases {
		got := stripPartitionSuffix(in)
		if got != want {
			t.Errorf("stripPartitionSuffix(%q) = %q, want %q", in, got, want)
		}
	}
}
