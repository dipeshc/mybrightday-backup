package processor

import (
	"bytes"
	"testing"

	mp4 "github.com/abema/go-mp4"
)

// encodeTestMP4 builds a minimal MP4 (ftyp + moov/mvhd), optionally with an
// existing moov/udta/©xyz atom carrying the given location string.
func encodeTestMP4(t *testing.T, existingLoc string) []byte {
	t.Helper()
	out := &memWriteSeeker{}
	w := mp4.NewWriter(out)

	writeBox := func(bt mp4.BoxType, payload mp4.IImmutableBox, children func()) {
		if _, err := w.StartBox(&mp4.BoxInfo{Type: bt}); err != nil {
			t.Fatalf("starting %s box: %v", bt, err)
		}
		if payload != nil {
			if _, err := mp4.Marshal(w, payload, mp4.Context{}); err != nil {
				t.Fatalf("marshaling %s box: %v", bt, err)
			}
		}
		if children != nil {
			children()
		}
		if _, err := w.EndBox(); err != nil {
			t.Fatalf("ending %s box: %v", bt, err)
		}
	}

	writeBox(mp4.BoxTypeFtyp(), &mp4.Ftyp{
		MajorBrand:       [4]byte{'i', 's', 'o', 'm'},
		MinorVersion:     512,
		CompatibleBrands: []mp4.CompatibleBrandElem{{CompatibleBrand: [4]byte{'i', 's', 'o', 'm'}}},
	}, nil)

	writeBox(mp4.BoxTypeMoov(), nil, func() {
		writeBox(mp4.BoxTypeMvhd(), &mp4.Mvhd{
			Timescale:   1000,
			Rate:        0x00010000,
			Volume:      0x0100,
			NextTrackID: 2,
		}, nil)
		if existingLoc != "" {
			writeBox(mp4.BoxTypeUdta(), nil, func() {
				if err := writeXyzBox(w, 0, 0); err != nil {
					t.Fatalf("writing existing ©xyz box: %v", err)
				}
			})
		}
	})

	return out.buf
}

// parseMP4Metadata re-parses MP4 bytes and returns the mvhd creation time and
// all ©xyz location strings found under moov/udta.
func parseMP4Metadata(t *testing.T, data []byte) (creation uint64, locs []string) {
	t.Helper()
	_, err := mp4.ReadBoxStructure(bytes.NewReader(data), func(h *mp4.ReadHandle) (any, error) {
		switch h.BoxInfo.Type {
		case mp4.BoxTypeMoov(), mp4.BoxTypeUdta():
			return h.Expand()
		case mp4.BoxTypeMvhd():
			box, _, err := h.ReadPayload()
			if err != nil {
				return nil, err
			}
			creation = box.(*mp4.Mvhd).GetCreationTime()
		case xyzBoxType:
			var buf bytes.Buffer
			if _, err := h.ReadData(&buf); err != nil {
				return nil, err
			}
			locs = append(locs, string(buf.Bytes()[4:]))
		}
		return nil, nil
	})
	if err != nil {
		t.Fatalf("parsing MP4: %v", err)
	}
	return creation, locs
}

func TestAddMP4Metadata(t *testing.T) {
	meta := testMeta()
	wantCreation := uint64(meta.DateTime.Unix() + mp4EpochOffset)
	wantLoc := "+37.4220-122.0841/"

	t.Run("no existing udta", func(t *testing.T) {
		out, err := AddMP4Metadata(encodeTestMP4(t, ""), meta)
		if err != nil {
			t.Fatalf("AddMP4Metadata: %v", err)
		}
		creation, locs := parseMP4Metadata(t, out)
		if creation != wantCreation {
			t.Errorf("creation time = %d, want %d", creation, wantCreation)
		}
		if len(locs) != 1 || locs[0] != wantLoc {
			t.Errorf("©xyz locations = %q, want [%q]", locs, wantLoc)
		}
	})

	t.Run("replaces existing ©xyz", func(t *testing.T) {
		out, err := AddMP4Metadata(encodeTestMP4(t, "+00.0000+00.0000/"), meta)
		if err != nil {
			t.Fatalf("AddMP4Metadata: %v", err)
		}
		_, locs := parseMP4Metadata(t, out)
		if len(locs) != 1 || locs[0] != wantLoc {
			t.Errorf("©xyz locations = %q, want [%q]", locs, wantLoc)
		}
	})

	t.Run("garbage input", func(t *testing.T) {
		if _, err := AddMP4Metadata([]byte("definitely not an mp4"), meta); err == nil {
			t.Error("expected error for garbage input")
		}
	})
}
