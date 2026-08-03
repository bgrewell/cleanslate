package slate

import "testing"

func TestParseSubvolumeLine(t *testing.T) {
	cases := []struct {
		line   string
		want   Subvolume
		wantOK bool
	}{
		{
			line:   "ID 256 gen 18 top level 5 parent_uuid - uuid abc12345-aaaa-bbbb-cccc-111111111111 path @baseline",
			want:   Subvolume{ID: 256, Generation: 18, ParentUUID: "", UUID: "abc12345-aaaa-bbbb-cccc-111111111111", Path: "@baseline"},
			wantOK: true,
		},
		{
			// btrfs-progs aligns columns with multiple spaces.
			line:   "ID 256 gen 31 top level 5 parent_uuid -                                    uuid cfc5c287-7fdf-e146-97cd-2285ea2be0c5 path @baseline",
			want:   Subvolume{ID: 256, Generation: 31, ParentUUID: "", UUID: "cfc5c287-7fdf-e146-97cd-2285ea2be0c5", Path: "@baseline"},
			wantOK: true,
		},
		{
			line:   "ID 258 gen 24 top level 5 parent_uuid abc12345-aaaa-bbbb-cccc-111111111111 uuid def45678-dddd-eeee-ffff-222222222222 path @runtime",
			want:   Subvolume{ID: 258, Generation: 24, ParentUUID: "abc12345-aaaa-bbbb-cccc-111111111111", UUID: "def45678-dddd-eeee-ffff-222222222222", Path: "@runtime"},
			wantOK: true,
		},
		{
			line:   "ID 260 gen 27 cgen 24 top level 5 parent_uuid abc12345-aaaa-bbbb-cccc-111111111111 received_uuid - uuid 11112222-3333-4444-5555-666666666666 path @gnb-test",
			want:   Subvolume{ID: 260, Generation: 27, ParentUUID: "abc12345-aaaa-bbbb-cccc-111111111111", UUID: "11112222-3333-4444-5555-666666666666", Path: "@gnb-test"},
			wantOK: true,
		},
		{line: "garbage line", wantOK: false},
		{line: "", wantOK: false},
	}

	for _, tc := range cases {
		got, ok := parseSubvolumeLine(tc.line)
		if ok != tc.wantOK {
			t.Errorf("parseSubvolumeLine(%q) ok=%v, want %v", tc.line, ok, tc.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if got != tc.want {
			t.Errorf("parseSubvolumeLine(%q):\n got: %+v\nwant: %+v", tc.line, got, tc.want)
		}
	}
}

func TestParseCmdlineSubvol(t *testing.T) {
	cases := []struct {
		cmdline string
		want    string
		wantOK  bool
	}{
		{cmdline: "BOOT_IMAGE=/vmlinuz root=PARTUUID=abc rootflags=subvol=@runtime console=ttyS0", want: "@runtime", wantOK: true},
		{cmdline: "rootflags=subvol=/@gnb-xyz", want: "@gnb-xyz", wantOK: true},
		{cmdline: "rootflags=compress=zstd,subvol=@baseline ro", want: "@baseline", wantOK: true},
		{cmdline: "rootflags=subvol=runtime", want: "@runtime", wantOK: true},
		{cmdline: "rootflags=compress=zstd ro", want: "", wantOK: false},
		{cmdline: "", want: "", wantOK: false},
	}
	for _, tc := range cases {
		got, ok := parseCmdlineSubvol(tc.cmdline)
		if got != tc.want || ok != tc.wantOK {
			t.Errorf("parseCmdlineSubvol(%q) = (%q, %v), want (%q, %v)", tc.cmdline, got, ok, tc.want, tc.wantOK)
		}
	}
}

func TestDisplayName(t *testing.T) {
	cases := map[string]string{
		"@baseline": "baseline",
		"@runtime":  "scratch",
		"@gnb-xyz":  "gnb-xyz",
		"@hostid":   "hostid", // hostid is filtered before displayName but the function still maps
		"@anything": "anything",
	}
	for in, want := range cases {
		if got := displayName(in); got != want {
			t.Errorf("displayName(%q) = %q, want %q", in, got, want)
		}
	}
}
