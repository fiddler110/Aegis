package security

import "testing"

// These fuzz targets cover the untrusted-input parsers this package feeds
// scanner output through: SARIF (shared by semgrep/trivy/grype/checkov/
// hadolint/ZAP), and the hand-written JSON/JSON-Lines/XML ingesters for
// tools that don't speak SARIF. All are pure functions over a byte slice
// with no side effects, so a fuzz failure here means a parser panics on
// malformed scanner output instead of returning an error — worth catching
// since scanner output is untrusted (a compromised or buggy scanner binary,
// or a crafted file that trips a scanner into emitting bad JSON/XML).

func FuzzParseSARIF(f *testing.F) {
	f.Add([]byte(`{"runs":[{"tool":{"driver":{"name":"grype","rules":[{"id":"CVE-1","properties":{"security-severity":"9.8"}}]}},"results":[{"ruleId":"CVE-1","level":"warning","message":{"text":"critical thing"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"go.mod"}}}]}]}]}`))
	f.Add([]byte(`{"runs":[{"tool":{"driver":{"name":"hadolint","rules":[{"id":"DL3000"}]}},"results":[{"ruleIndex":0,"level":"error","message":{"text":""},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"Dockerfile"},"region":{"startLine":3}}}]}]}]}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(``))
	f.Add([]byte(`{"runs":[{"results":[{"ruleIndex":99}]}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		findings, err := ParseSARIF(data, "test-tool")
		if err != nil {
			return
		}
		for _, fnd := range findings {
			_ = string(fnd.Severity)
		}
	})
}

func FuzzParseGitleaks(f *testing.F) {
	f.Add([]byte(`[{"RuleID":"generic-api-key","Description":"secret","File":"a.go","StartLine":5}]`))
	f.Add([]byte(`[]`))
	f.Add([]byte(``))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = parseGitleaks(data)
	})
}

func FuzzParseTrufflehog(f *testing.F) {
	f.Add([]byte(`{"DetectorName":"AWS","Verified":true,"Redacted":"AKIA****","SourceMetadata":{"Data":{"Filesystem":{"file":"a.go","line":3}}}}`))
	f.Add([]byte("\n\n"))
	f.Add([]byte(``))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = parseTrufflehog(data, true)
		_, _ = parseTrufflehog(data, false)
	})
}

func FuzzParseOSVScanner(f *testing.F) {
	f.Add([]byte(`{"results":[{"source":{"path":"go.mod"},"packages":[{"package":{"name":"foo","version":"1.0.0","ecosystem":"Go"},"vulnerabilities":[{"id":"GHSA-1","summary":"bad"}],"groups":[{"ids":["GHSA-1"],"max_severity":"7.5"}]}]}]}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(``))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = parseOSVScanner(data)
	})
}

func FuzzParseNmapXML(f *testing.F) {
	f.Add([]byte(`<?xml version="1.0"?>
<nmaprun>
  <host>
    <address addr="192.168.1.10" addrtype="ipv4"/>
    <ports>
      <port protocol="tcp" portid="80">
        <state state="open"/>
        <service name="http" product="nginx" version="1.25"/>
      </port>
    </ports>
  </host>
</nmaprun>`))
	f.Add([]byte(`<nmaprun></nmaprun>`))
	f.Add([]byte(``))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = parseNmapXML(data)
	})
}
