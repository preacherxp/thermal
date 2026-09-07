package thermal

import (
	"bytes"
	"compress/zlib"
	_ "embed"
	"encoding/binary"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf16"
)

// PDF reference: https://opensource.adobe.com/dc-acrobat-sdk-docs/pdfstandards/pdfreference1.7old.pdf
//
//go:embed assets/Go-Regular.ttf
var pdfFontData []byte

const pdfWidth = 595.28
const pdfHeight = 841.89
const pdfMargin = 46.0
const pdfContentWidth = pdfWidth - 2*pdfMargin

type pdfColor struct{ r, g, b float64 }

var pdfInk = pdfColor{0.13, 0.18, 0.21}
var pdfMuted = pdfColor{0.43, 0.48, 0.50}
var pdfAccent = pdfColor{0.05, 0.43, 0.41}
var pdfAmber = pdfColor{0.61, 0.34, 0.12}
var pdfRule = pdfColor{0.85, 0.88, 0.88}
var pdfPaper = pdfColor{0.96, 0.97, 0.96}

type pdfFont struct {
	data, cmap, hmtx []byte
	units, metrics   uint16
	ascent, descent  int16
	box              [4]int16
	used             map[uint16]rune
}

func pdfU16(b []byte, i int) uint16 { return binary.BigEndian.Uint16(b[i : i+2]) }
func pdfU32(b []byte, i int) uint32 { return binary.BigEndian.Uint32(b[i : i+4]) }

func newPDFFont() (*pdfFont, error) {
	data := pdfFontData
	if len(data) < 12 {
		return nil, fmt.Errorf("embedded PDF font is invalid")
	}
	tables := map[string][]byte{}
	n := int(pdfU16(data, 4))
	if len(data) < 12+n*16 {
		return nil, fmt.Errorf("embedded PDF font directory is invalid")
	}
	for i := 0; i < n; i++ {
		p := 12 + i*16
		off, size := uint64(pdfU32(data, p+8)), uint64(pdfU32(data, p+12))
		if off+size > uint64(len(data)) {
			return nil, fmt.Errorf("embedded PDF font table is invalid")
		}
		tables[string(data[p:p+4])] = data[off : off+size]
	}
	head, hhea, cmap := tables["head"], tables["hhea"], tables["cmap"]
	if len(head) < 54 || len(hhea) < 36 || len(cmap) < 4 {
		return nil, fmt.Errorf("embedded PDF font metrics are missing")
	}
	f := &pdfFont{data: data, units: pdfU16(head, 18), metrics: pdfU16(hhea, 34), hmtx: tables["hmtx"], ascent: int16(pdfU16(hhea, 4)), descent: int16(pdfU16(hhea, 6)), used: map[uint16]rune{}}
	if f.units == 0 || f.metrics == 0 || len(f.hmtx) < int(f.metrics)*4 {
		return nil, fmt.Errorf("embedded PDF font metrics are invalid")
	}
	for i := 0; i < 4; i++ {
		f.box[i] = int16(pdfU16(head, 36+i*2))
	}
	count := int(pdfU16(cmap, 2))
	if len(cmap) < 4+count*8 {
		return nil, fmt.Errorf("embedded PDF font cmap is invalid")
	}
	for i := 0; i < count; i++ {
		p := 4 + i*8
		platform, encoding := pdfU16(cmap, p), pdfU16(cmap, p+2)
		if platform != 0 && !(platform == 3 && (encoding == 1 || encoding == 10)) {
			continue
		}
		off := int(pdfU32(cmap, p+4))
		if off < 0 || off+2 > len(cmap) {
			continue
		}
		format := pdfU16(cmap, off)
		if format == 4 && off+4 <= len(cmap) {
			size := int(pdfU16(cmap, off+2))
			if size >= 16 && off+size <= len(cmap) {
				f.cmap = cmap[off : off+size]
			}
		}
		if format == 12 && off+16 <= len(cmap) {
			size := int(pdfU32(cmap, off+4))
			if size >= 16 && off+size <= len(cmap) {
				f.cmap = cmap[off : off+size]
				break
			}
		}
	}
	if len(f.cmap) == 0 {
		return nil, fmt.Errorf("embedded PDF font has no Unicode cmap")
	}
	return f, nil
}
func (f *pdfFont) glyph(r rune) uint16 {
	b := f.cmap
	if pdfU16(b, 0) == 12 {
		count := int(pdfU32(b, 12))
		for i := 0; i < count && 16+(i+1)*12 <= len(b); i++ {
			p := 16 + i*12
			first, last := pdfU32(b, p), pdfU32(b, p+4)
			if uint32(r) >= first && uint32(r) <= last {
				return uint16(pdfU32(b, p+8) + uint32(r) - first)
			}
		}
	} else if r <= 0xffff {
		n := int(pdfU16(b, 6)) / 2
		if 16+n*8 > len(b) {
			return 0
		}
		for i := 0; i < n; i++ {
			end := pdfU16(b, 14+2*i)
			start := pdfU16(b, 16+2*n+2*i)
			if uint32(r) < uint32(start) || uint32(r) > uint32(end) {
				continue
			}
			delta := pdfU16(b, 16+4*n+2*i)
			pos := 16 + 6*n + 2*i
			off := int(pdfU16(b, pos))
			if off == 0 {
				return uint16(r) + delta
			}
			pos += off + 2*(int(r)-int(start))
			if pos+2 > len(b) {
				return 0
			}
			glyph := pdfU16(b, pos)
			if glyph != 0 {
				glyph += delta
			}
			return glyph
		}
	}
	return 0
}
func (f *pdfFont) mapped(r rune) (uint16, rune) {
	g := f.glyph(r)
	if g == 0 {
		r = '?'
		g = f.glyph(r)
	}
	return g, r
}
func (f *pdfFont) advance(g uint16) float64 {
	index := int(g)
	if index >= int(f.metrics) {
		index = int(f.metrics) - 1
	}
	return float64(pdfU16(f.hmtx, index*4)) * 1000 / float64(f.units)
}
func (f *pdfFont) width(s string, size float64) float64 {
	w := 0.0
	for _, r := range clean(s) {
		g, _ := f.mapped(r)
		w += f.advance(g) * size / 1000
	}
	return w
}
func (f *pdfFont) encode(s string) string {
	var b strings.Builder
	for _, r := range clean(s) {
		g, mapped := f.mapped(r)
		f.used[g] = mapped
		fmt.Fprintf(&b, "%04X", g)
	}
	return b.String()
}
func (f *pdfFont) wrap(s string, size, width float64) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(clean(s)) {
		if line != "" && f.width(line+" "+word, size) <= width {
			line += " " + word
			continue
		}
		if line != "" {
			lines = append(lines, line)
			line = ""
		}
		for _, r := range word {
			if line != "" && f.width(line+string(r), size) > width {
				lines = append(lines, line)
				line = ""
			}
			line += string(r)
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

type pdfPage struct {
	bytes.Buffer
	font *pdfFont
}

func (p *pdfPage) text(x, y, size float64, c pdfColor, s string) {
	mode := "0 Tr"

	fmt.Fprintf(p, "q %.3f %.3f %.3f rg %.3f %.3f %.3f RG BT /F1 %.2f Tf %s 1 0 0 1 %.2f %.2f Tm <%s> Tj ET Q\n", c.r, c.g, c.b, c.r, c.g, c.b, size, mode, x, pdfHeight-y-size, p.font.encode(s))
}
func (p *pdfPage) rect(x, y, w, h float64, c pdfColor) {
	fmt.Fprintf(p, "q %.3f %.3f %.3f rg %.2f %.2f %.2f %.2f re f Q\n", c.r, c.g, c.b, x, pdfHeight-y-h, w, h)
}
func (p *pdfPage) line(x1, y1, x2, y2, width float64, c pdfColor) {
	fmt.Fprintf(p, "q %.3f %.3f %.3f RG %.2f w %.2f %.2f m %.2f %.2f l S Q\n", c.r, c.g, c.b, width, x1, pdfHeight-y1, x2, pdfHeight-y2)
}

type pdfDocument struct {
	font  *pdfFont
	pages []*pdfPage
}

func (d *pdfDocument) page() *pdfPage {
	p := &pdfPage{font: d.font}
	d.pages = append(d.pages, p)
	return p
}
func pdfStream(data []byte, extra string) []byte {
	return append(append([]byte(fmt.Sprintf("<< /Length %d %s >>\nstream\n", len(data), extra)), data...), []byte("\nendstream")...)
}
func pdfCompressed(data []byte, extra string) []byte {
	var b bytes.Buffer
	z := zlib.NewWriter(&b)
	_, _ = z.Write(data)
	_ = z.Close()
	return pdfStream(b.Bytes(), "/Filter /FlateDecode "+extra)
}
func (d *pdfDocument) write(w io.Writer) error {
	if len(d.pages) == 0 || len(d.pages) > 200 {
		return fmt.Errorf("PDF report must contain 1 to 200 pages")
	}
	for i, p := range d.pages {
		p.line(pdfMargin, pdfHeight-48, pdfWidth-pdfMargin, pdfHeight-48, 0.5, pdfRule)
		p.text(pdfMargin, pdfHeight-35, 8, pdfMuted, "THERMAL  /  Local measurements")
		label := fmt.Sprintf("%02d / %02d", i+1, len(d.pages))
		p.text(pdfWidth-pdfMargin-d.font.width(label, 8), pdfHeight-35, 8, pdfMuted, label)
	}
	objects := make([][]byte, 8)
	objects[0] = []byte("<< /Type /Catalog /Pages 2 0 R /Lang (en) >>")
	var kids strings.Builder
	for i := range d.pages {
		fmt.Fprintf(&kids, "%d 0 R ", 9+i*2)
	}
	objects[1] = []byte(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids.String(), len(d.pages)))
	objects[2] = []byte("<< /Type /Font /Subtype /Type0 /BaseFont /GoRegular /Encoding /Identity-H /DescendantFonts [4 0 R] /ToUnicode 8 0 R >>")
	var widths strings.Builder
	glyphs := make([]int, 0, len(d.font.used))
	for g := range d.font.used {
		glyphs = append(glyphs, int(g))
	}
	sort.Ints(glyphs)
	for _, g := range glyphs {
		fmt.Fprintf(&widths, "%d [%.2f] ", g, d.font.advance(uint16(g)))
	}
	objects[3] = []byte(fmt.Sprintf("<< /Type /Font /Subtype /CIDFontType2 /BaseFont /GoRegular /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor 5 0 R /CIDToGIDMap /Identity /DW 1000 /W [%s] >>", widths.String()))
	scale := 1000 / float64(d.font.units)
	box := d.font.box
	objects[4] = []byte(fmt.Sprintf("<< /Type /FontDescriptor /FontName /GoRegular /Flags 32 /FontBBox [%.0f %.0f %.0f %.0f] /ItalicAngle 0 /Ascent %.0f /Descent %.0f /CapHeight %.0f /StemV 80 /FontFile2 6 0 R >>", float64(box[0])*scale, float64(box[1])*scale, float64(box[2])*scale, float64(box[3])*scale, float64(d.font.ascent)*scale, float64(d.font.descent)*scale, float64(d.font.ascent)*scale))
	objects[5] = pdfCompressed(d.font.data, fmt.Sprintf("/Length1 %d", len(d.font.data)))
	objects[6] = []byte("<< /Title (Thermal findings) /Creator (Thermal CLI) >>")
	var cmap strings.Builder
	cmap.WriteString("/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n/CMapName /ThermalUnicode def\n/CMapType 2 def\n1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n")
	for start := 0; start < len(glyphs); start += 100 {
		end := min(start+100, len(glyphs))
		fmt.Fprintf(&cmap, "%d beginbfchar\n", end-start)
		for _, g := range glyphs[start:end] {
			fmt.Fprintf(&cmap, "<%04X> <", g)
			for _, u := range utf16.Encode([]rune{d.font.used[uint16(g)]}) {
				fmt.Fprintf(&cmap, "%04X", u)
			}
			cmap.WriteString(">\n")
		}
		cmap.WriteString("endbfchar\n")
	}
	cmap.WriteString("endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend")
	objects[7] = pdfStream([]byte(cmap.String()), "")
	for i, p := range d.pages {
		objects = append(objects, []byte(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.2f %.2f] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", pdfWidth, pdfHeight, 10+i*2)), pdfStream(p.Bytes(), ""))
	}
	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	offsets := make([]int, len(objects))
	for i, obj := range objects {
		offsets[i] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n", i+1)
		out.Write(obj)
		out.WriteString("\nendobj\n")
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R /Info 7 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	_, err := io.Copy(w, &out)
	return err
}
