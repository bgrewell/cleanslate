package slate

import "testing"

func TestRewriteBLSEntry(t *testing.T) {
	template := `title    cleanslate: fresh (ephemeral)
version  6.8.0-111-generic
linux    /vmlinuz-6.8.0-111-generic
initrd   /initrd.img-6.8.0-111-generic
options  root=PARTUUID=abc ro console=ttyS0,115200 console=tty0 rootflags=subvol=@runtime
`
	got := rewriteBLSEntry(template, "gnb-xyz", "@gnb-xyz")
	want := `title    cleanslate: gnb-xyz
version  6.8.0-111-generic
linux    /vmlinuz-6.8.0-111-generic
initrd   /initrd.img-6.8.0-111-generic
options  root=PARTUUID=abc ro console=ttyS0,115200 console=tty0 rootflags=subvol=@gnb-xyz
`
	if got != want {
		t.Errorf("rewriteBLSEntry mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestRewriteOptionsLine(t *testing.T) {
	cases := []struct {
		in     string
		subvol string
		want   string
	}{
		{
			in:     "options  root=PARTUUID=abc rootflags=subvol=@runtime ro",
			subvol: "@gnb-xyz",
			want:   "options  root=PARTUUID=abc rootflags=subvol=@gnb-xyz ro",
		},
		{
			in:     "options  rootflags=compress=zstd,subvol=@runtime ro",
			subvol: "@base",
			want:   "options  rootflags=compress=zstd,subvol=@base ro",
		},
		{
			// no rootflags at all → append one
			in:     "options  root=PARTUUID=abc ro",
			subvol: "@base",
			want:   "options  root=PARTUUID=abc ro rootflags=subvol=@base",
		},
		{
			// rootflags without subvol component → add subvol
			in:     "options  rootflags=compress=zstd ro",
			subvol: "@gnb",
			want:   "options  rootflags=compress=zstd,subvol=@gnb ro",
		},
	}
	for _, tc := range cases {
		got := rewriteOptionsLine(tc.in, tc.subvol)
		if got != tc.want {
			t.Errorf("rewriteOptionsLine(%q, %q):\n got: %q\nwant: %q", tc.in, tc.subvol, got, tc.want)
		}
	}
}
