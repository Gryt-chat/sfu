package svc

import (
	"testing"
)

func TestParseMandatoryOnly(t *testing.T) {
	p := NewDDParser()

	// 3-byte mandatory header: start_of_frame=1, end_of_frame=1, template_id=5, frame_number=0x0102
	data := []byte{0xC5, 0x01, 0x02}
	fi, err := p.Parse(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fi.StartOfFrame {
		t.Error("expected StartOfFrame=true")
	}
	if !fi.EndOfFrame {
		t.Error("expected EndOfFrame=true")
	}
	if fi.TemplateID != 5 {
		t.Errorf("expected TemplateID=5, got %d", fi.TemplateID)
	}
	if fi.FrameNumber != 0x0102 {
		t.Errorf("expected FrameNumber=0x0102, got %d", fi.FrameNumber)
	}
	if fi.TemporalID != -1 {
		t.Errorf("expected TemporalID=-1 (unknown), got %d", fi.TemporalID)
	}
}

func TestParseTooShort(t *testing.T) {
	p := NewDDParser()
	_, err := p.Parse([]byte{0x00, 0x00})
	if err == nil {
		t.Error("expected error for 2-byte payload")
	}
}

func TestParseWithTemplateStructure(t *testing.T) {
	p := NewDDParser()

	// Build a synthetic DD payload with a template dependency structure.
	// Mandatory header: start=1 end=1 template_id=0 frame=0
	// Byte 3: template_dependency_structure_present_flag=1, active_decode_targets_present_flag=0, template_id_offset=0
	// Then: dt_cnt_minus1=0 (5 bits = 00000) → dt_cnt=1
	// Templates (L1T3): 3 templates
	//   T0: spatial=0 (00), temporal=0 (000) → bits: 00 000
	//   T1: spatial=0 (00), temporal=1 (001) → bits: 00 001
	//   T2: spatial=0 (00), temporal=2 (010) → bits: 00 010
	//   End marker: spatial=0 (00), temporal=0 (000) → bits: 00 000
	//
	// Bit layout after mandatory 3 bytes:
	//   Byte 3: [1][0][000000]  = 0x80 (TDS present, no active targets, offset=0)
	//   Byte 4: [00000][00 00] = dt_cnt-1=0 (5 bits), then T0 spatial(2)+temporal(3) starts
	//     dt_cnt-1 = 00000
	//     T0: 00 000
	//     so byte 4: 00000_00 0  = next bits continue to byte 5
	//   Let me lay it out bit by bit:
	//
	// Byte 3 (bits 0-7):   1 0 000000    → TDS=1, active=0, offset=0
	// Byte 4 (bits 8-15):  00000 00 000  → dtCnt-1=0, T0: s=0 t=0 (first 2+3=5 bits)
	//   Actually bits 8-12: 00000 (dtCnt=1)
	//   bits 13-14: 00 (T0 spatial)
	//   bit 15: 0 (T0 temporal bit0)
	// Byte 5 (bits 16-23): 00 00 001 00  → T0 temporal bits 1-2 = 00, T1: s=0 t=1
	//   bits 16-17: 00 (T0 temporal bits 1-2)
	//   bits 18-19: 00 (T1 spatial)
	//   bits 20-22: 001 (T1 temporal = 1)
	//   bit 23: 0 (T2 spatial bit0)
	// Byte 6 (bits 24-31): 0 010 00 000  → T2 spatial bit1=0, T2 temporal=2, end marker s=0 t=0
	//   bit 24: 0 (T2 spatial bit1)
	//   bits 25-27: 010 (T2 temporal = 2)
	//   bits 28-29: 00 (end marker spatial)
	//   bits 30-32: 000 (end marker temporal)
	//
	// So:
	// Byte 3: 10000000 = 0x80
	// Byte 4: 00000_00_0 = bits: 00000 00 0 = 0x00
	// Byte 5: 00 00 001 0 = 0x02
	// Byte 6: 0 010 00 000 = 0x40
	// Byte 7: pad

	// Bit layout of the TDS portion (bytes 3+):
	//   Byte 3: [1][0][000000]                        = 0x80
	//   Byte 4: [00000][00][0]                         = 0x00
	//           dtCnt-1  T0s  T0t(MSB)
	//   Byte 5: [00][00][001][0]                       = 0x02
	//           T0t T1s  T1t  T2s(MSB)
	//   Byte 6: [0][010][00][000]                      = 0x20
	//           T2s T2t  Es   Et
	//   Byte 7: [0...]                                 = 0x00
	data := []byte{
		0xC0, 0x00, 0x00, // mandatory: start=1 end=1 template_id=0 frame=0
		0x80, // TDS present, no active decode targets, offset=0
		0x00, // dtCnt-1=0, T0: spatial=00, temporal MSB=0
		0x02, // T0 temporal done=00, T1: spatial=00, temporal=001, T2 spatial MSB=0
		0x20, // T2 spatial LSB=0, T2 temporal=010, end: spatial=00, temporal=000
		0x00, // end temporal LSB + pad
	}

	fi, err := p.Parse(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fi.TemplateID != 0 {
		t.Errorf("expected TemplateID=0, got %d", fi.TemplateID)
	}

	// Template 0 should map to temporal_id=0
	if fi.TemporalID != 0 {
		t.Errorf("expected TemporalID=0, got %d", fi.TemporalID)
	}

	// Now verify subsequent packets with just the mandatory header resolve correctly
	testCases := []struct {
		templateID     uint8
		wantTemporalID int
	}{
		{0, 0},
		{1, 1},
		{2, 2},
	}

	for _, tc := range testCases {
		pkt := []byte{tc.templateID, 0x00, 0x01}
		fi2, err := p.Parse(pkt)
		if err != nil {
			t.Fatalf("unexpected error for template_id=%d: %v", tc.templateID, err)
		}
		if fi2.TemporalID != tc.wantTemporalID {
			t.Errorf("template_id=%d: want temporalID=%d, got %d", tc.templateID, tc.wantTemporalID, fi2.TemporalID)
		}
	}
}

func TestBitReader(t *testing.T) {
	data := []byte{0b11010110, 0b01011010}
	r := bitReader{data: data, pos: 0}

	// Read 3 bits: 110
	v := r.readBits(3)
	if v != 6 {
		t.Errorf("expected 6 (110), got %d", v)
	}

	// Read 5 bits: 10110
	v = r.readBits(5)
	if v != 22 {
		t.Errorf("expected 22 (10110), got %d", v)
	}

	// Read 8 bits: 01011010
	v = r.readBits(8)
	if v != 0x5A {
		t.Errorf("expected 0x5A, got 0x%02X", v)
	}
}

func TestHasTemplates(t *testing.T) {
	p := NewDDParser()
	if p.HasTemplates() {
		t.Error("expected no templates initially")
	}
}
