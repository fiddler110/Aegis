package hwinfo

import "testing"

// TestDetectAlwaysReportsCPUCores checks runtime.NumCPU() is always wired in,
// on every platform this test runs on — no fail-soft path exists for CPU
// core count, unlike RAM.
func TestDetectAlwaysReportsCPUCores(t *testing.T) {
	info := Detect()
	if info.CPUCores < 1 {
		t.Fatalf("CPUCores = %d, want >= 1", info.CPUCores)
	}
}

// TestInfoRAMKnown exercises the fail-soft contract: RAMKnown/TotalRAMGB must
// treat "unknown" or zero-byte results as not-known, regardless of platform,
// so callers (modelcatalog.RecommendLocal, the CLI/TUI) can safely degrade to
// hardware-agnostic guidance without special-casing each Source value.
func TestInfoRAMKnown(t *testing.T) {
	cases := []struct {
		name string
		info Info
		want bool
	}{
		{"unknown source, zero bytes", Info{RAMSource: SourceUnknown, TotalRAMBytes: 0}, false},
		{"zero source value, zero bytes", Info{TotalRAMBytes: 0}, false},
		{"known source but zero bytes", Info{RAMSource: SourceProcMeminfo, TotalRAMBytes: 0}, false},
		{"known source with bytes", Info{RAMSource: SourceProcMeminfo, TotalRAMBytes: 16 << 30}, true},
		{"sysctl source with bytes", Info{RAMSource: SourceSysctl, TotalRAMBytes: 8 << 30}, true},
		{"windows source with bytes", Info{RAMSource: SourceWinAPI, TotalRAMBytes: 32 << 30}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.info.RAMKnown(); got != tc.want {
				t.Errorf("RAMKnown() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestInfoTotalRAMGB(t *testing.T) {
	known := Info{RAMSource: SourceProcMeminfo, TotalRAMBytes: 16 << 30}
	if got := known.TotalRAMGB(); got != 16 {
		t.Errorf("TotalRAMGB() = %v, want 16", got)
	}
	unknown := Info{RAMSource: SourceUnknown}
	if got := unknown.TotalRAMGB(); got != 0 {
		t.Errorf("TotalRAMGB() for unknown = %v, want 0", got)
	}
}

func TestInfoDescribe(t *testing.T) {
	known := Info{CPUCores: 8, RAMSource: SourceProcMeminfo, TotalRAMBytes: 16 << 30}
	if got := known.Describe(); got == "" {
		t.Error("Describe() returned empty string for known info")
	}
	unknown := Info{CPUCores: 4, RAMSource: SourceUnknown}
	got := unknown.Describe()
	if got == "" {
		t.Error("Describe() returned empty string for unknown RAM")
	}
	// Must not silently claim a real detection when RAM is unknown.
	if got == known.Describe() {
		t.Error("Describe() for unknown RAM must differ from a known result")
	}
}

// TestParseMemTotalKB is fully portable (pure string parsing, no OS
// dependency) even though it backs the Linux-only totalRAM() code path.
func TestParseMemTotalKB(t *testing.T) {
	cases := []struct {
		name   string
		data   string
		wantKB uint64
		wantOK bool
	}{
		{
			name:   "typical /proc/meminfo",
			data:   "MemTotal:       16384000 kB\nMemFree:         1234567 kB\n",
			wantKB: 16384000,
			wantOK: true,
		},
		{
			name:   "MemTotal not first line",
			data:   "SomeOtherField:  1 kB\nMemTotal:  8192000 kB\n",
			wantKB: 8192000,
			wantOK: true,
		},
		{
			name:   "missing MemTotal",
			data:   "MemFree: 1234 kB\n",
			wantKB: 0,
			wantOK: false,
		},
		{
			name:   "empty input",
			data:   "",
			wantKB: 0,
			wantOK: false,
		},
		{
			name:   "malformed value",
			data:   "MemTotal: notanumber kB\n",
			wantKB: 0,
			wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kb, ok := parseMemTotalKB([]byte(tc.data))
			if ok != tc.wantOK || kb != tc.wantKB {
				t.Errorf("parseMemTotalKB(%q) = (%d, %v), want (%d, %v)", tc.data, kb, ok, tc.wantKB, tc.wantOK)
			}
		})
	}
}

// TestParseSysctlMemsize is fully portable even though it backs the
// Darwin-only totalRAM() code path.
func TestParseSysctlMemsize(t *testing.T) {
	cases := []struct {
		name   string
		out    string
		want   uint64
		wantOK bool
	}{
		{"typical output", "17179869184\n", 17179869184, true},
		{"no trailing newline", "8589934592", 8589934592, true},
		{"whitespace padded", "  4294967296  \n", 4294967296, true},
		{"empty", "", 0, false},
		{"garbage", "not-a-number\n", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseSysctlMemsize([]byte(tc.out))
			if ok != tc.wantOK || got != tc.want {
				t.Errorf("parseSysctlMemsize(%q) = (%d, %v), want (%d, %v)", tc.out, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}
