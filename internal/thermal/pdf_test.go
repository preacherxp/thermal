package thermal

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"
)

func pdfTextForTest(t *testing.T, data []byte) string {
	t.Helper()
	cmap := map[string]string{}
	for _, m := range regexp.MustCompile(`<([0-9A-F]{4})> <([0-9A-F]{4,8})>`).FindAllStringSubmatch(string(data), -1) {
		raw, err := hex.DecodeString(m[2])
		if err != nil {
			t.Fatal(err)
		}
		var units []uint16
		for i := 0; i < len(raw); i += 2 {
			units = append(units, uint16(raw[i])<<8|uint16(raw[i+1]))
		}
		cmap[m[1]] = string(utf16.Decode(units))
	}
	var text strings.Builder
	for _, m := range regexp.MustCompile(`<([0-9A-F]+)> Tj`).FindAllStringSubmatch(string(data), -1) {
		for i := 0; i < len(m[1]); i += 4 {
			text.WriteString(cmap[m[1][i:i+4]])
		}
		text.WriteByte('\n')
	}
	return text.String()
}
func assertPDFStructure(t *testing.T, data []byte) {
	t.Helper()
	if !bytes.HasPrefix(data, []byte("%PDF-1.4")) || !bytes.HasSuffix(data, []byte("%%EOF\n")) {
		t.Fatal("invalid PDF envelope")
	}
	marker := bytes.LastIndex(data, []byte("startxref\n"))
	if marker < 0 {
		t.Fatal("no xref pointer")
	}
	fields := strings.Fields(string(data[marker+10:]))
	offset, err := strconv.Atoi(fields[0])
	if err != nil || offset >= len(data) {
		t.Fatal("invalid xref offset")
	}
	lines := strings.Split(string(data[offset:]), "\n")
	if lines[0] != "xref" {
		t.Fatal("xref offset does not point to xref")
	}
	var first, count int
	if _, err := fmt.Sscanf(lines[1], "%d %d", &first, &count); err != nil || first != 0 {
		t.Fatal("invalid xref")
	}
	for i := 1; i < count; i++ {
		var off, gen int
		var flag string
		if _, err := fmt.Sscanf(lines[i+2], "%d %d %s", &off, &gen, &flag); err != nil || off >= len(data) {
			t.Fatal("invalid object offset")
		}
		if !bytes.HasPrefix(data[off:], []byte(fmt.Sprintf("%d 0 obj\n", i))) {
			t.Fatalf("object %d is misplaced", i)
		}
	}
	if !bytes.Contains(data, []byte("/FontFile2")) || !bytes.Contains(data, []byte("/ToUnicode")) {
		t.Fatal("font or Unicode map not embedded")
	}
	for _, m := range regexp.MustCompile(`1 0 0 1 ([0-9.-]+) ([0-9.-]+) Tm`).FindAllStringSubmatch(string(data), -1) {
		x, _ := strconv.ParseFloat(m[1], 64)
		y, _ := strconv.ParseFloat(m[2], 64)
		if x < 0 || x > pdfWidth || y < 0 || y > pdfHeight {
			t.Fatalf("text outside page: %v", m)
		}
	}
}
func TestPDFFindingsPreserveUnknownTargets(t *testing.T) {
	cpu := fixture("baseline", 60, 20, 1000)
	cpu.Workload = "cpu-sha256-v1"
	cpu.Operations = 1000
	cpu.Elapsed = 1
	findings := pdfFindings(cpu)
	if len(findings) != 1 || !strings.Contains(findings[0].title, "temperature unknown") {
		t.Fatalf("GPU reading was used as CPU evidence: %+v", findings)
	}
	gpu := fixture("baseline", 87, 100, 1800)
	gpu.Workload = "gpu-integer-v1"
	gpu.GPU = &GPUResult{Device: "GPU", Backend: "test", Workload: "gpu-integer-v1", Iterations: 2000, Elapsed: 1, Verified: true}
	f := pdfFindings(gpu)
	if len(f) != 1 || !f[0].caution || !strings.Contains(f[0].body, "87.0 °C") || !strings.Contains(f[0].body, "throttling") {
		t.Fatalf("missing thermal evidence: %+v", f)
	}
	cpu.Status = "refused"
	cpu.Operations = 0
	if f := pdfFindings(cpu); len(f) != 1 || !strings.Contains(f[0].title, "incomplete") {
		t.Fatalf("refusal presented as a successful measurement: %+v", f)
	}
}
func TestPDFReportsAndUnicode(t *testing.T) {
	r := fixture("baseline", 60, 20, 1000)
	r.Host = "Łódź"
	r.Notes = "Zażółć gęślą jaźń (PDF) \\ record"
	for _, after := range []*Run{nil, &r} {
		var b bytes.Buffer
		if err := WriteReportPDF(&b, r, after); err != nil {
			t.Fatal(err)
		}
		assertPDFStructure(t, b.Bytes())
		text := pdfTextForTest(t, b.Bytes())
		if !strings.Contains(text, "Łódź") {
			t.Fatalf("Unicode hostname missing: %s", text)
		}
		if after == nil && !strings.Contains(text, r.Notes) {
			t.Fatal("notes lost or escaped incorrectly")
		}
	}
}
func TestPDFLongNotesPaginate(t *testing.T) {
	r := fixture("baseline", 60, 20, 1000)
	r.Host = strings.Repeat("long-host-", 40)
	r.Notes = strings.Repeat("A measured observation with context. ", 500) + "END OF NOTES"
	var b bytes.Buffer
	if err := WriteReportPDF(&b, r, nil); err != nil {
		t.Fatal(err)
	}
	assertPDFStructure(t, b.Bytes())
	if !strings.Contains(pdfTextForTest(t, b.Bytes()), "END OF NOTES") {
		t.Fatal("long notes were truncated")
	}
	if bytes.Count(b.Bytes(), []byte("/Type /Page ")) < 3 {
		t.Fatal("long report was not paginated")
	}
}
func TestPDFPreservesFilesAndWriterErrors(t *testing.T) {
	r := fixture("baseline", 60, 20, 1000)
	path := filepath.Join(t.TempDir(), "report.pdf")
	if err := SaveReportPDF(path, r, nil); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	if err := SaveReportPDF(path, r, nil); !errors.Is(err, os.ErrExist) {
		t.Fatalf("overwrite not refused: %v", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("existing PDF changed")
	}
	if err := SaveReportPDF(filepath.Join(t.TempDir(), "wrong.png"), r, nil); err == nil {
		t.Fatal("wrong extension accepted")
	}
	if err := WriteReportPDF(brokenPNGWriter{}, r, nil); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("writer error lost: %v", err)
	}
}

func TestPDFRecordingAssessesEachTarget(t *testing.T) {
	r := fixture("baseline", 60, 20, 1000)
	for i := range r.Samples {
		r.Samples[i].Devices = []Device{{ID: "cpu", Kind: "cpu", Name: "CPU", Temp: Number(84)}, {ID: "gpu", Kind: "gpu", Name: "GPU", Temp: Number(81)}}
	}
	var out bytes.Buffer
	if err := WriteReportPDF(&out, r, nil); err != nil {
		t.Fatal(err)
	}
	text := pdfTextForTest(t, out.Bytes())
	if !strings.Contains(text, "GPU: Elevated thermal readings") || !strings.Contains(text, "CPU: No elevated sustained temperature observed") {
		t.Fatalf("missing target-specific findings: %s", text)
	}
	r.Samples = []Sample{{}}
	findings := pdfFindings(r)
	if len(findings) != 1 || !strings.Contains(findings[0].title, "temperature unknown") {
		t.Fatalf("missing readings hidden: %+v", findings)
	}
	if got := pdfNumber(Number(1e9), "", true); got != "1.00 G" {
		t.Fatalf("inconsistent SI prefix: %s", got)
	}
}
