package processor

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	mp4 "github.com/abema/go-mp4"
)

// mp4Epoch converts a Unix timestamp to the MP4 epoch (seconds since 1904-01-01 UTC).
const mp4EpochOffset = 2082844800

// xyzBoxType is the QuickTime user-data location atom (moov/udta/©xyz),
// which Google Photos and Apple Photos read for GPS coordinates.
var xyzBoxType = mp4.StrToBoxType("\xa9xyz")

// AddMP4Metadata injects the capture time (moov/mvhd creation/modification
// time) and GPS location (moov/udta/©xyz) into MP4 data. Any pre-existing
// ©xyz atom is replaced. meta.TimezoneOffset is unused: mvhd times are UTC
// by definition.
func AddMP4Metadata(data []byte, meta PhotoMeta) ([]byte, error) {
	r := bytes.NewReader(data)
	out := &memWriteSeeker{}
	w := mp4.NewWriter(out)

	creation := uint64(meta.DateTime.Unix() + mp4EpochOffset)
	sawMvhd := false
	sawUdta := false

	_, err := mp4.ReadBoxStructure(r, func(h *mp4.ReadHandle) (any, error) {
		switch h.BoxInfo.Type {
		case mp4.BoxTypeMoov():
			if _, err := w.StartBox(&h.BoxInfo); err != nil {
				return nil, err
			}
			if _, err := h.Expand(); err != nil {
				return nil, err
			}
			if !sawUdta {
				if _, err := w.StartBox(&mp4.BoxInfo{Type: mp4.BoxTypeUdta()}); err != nil {
					return nil, err
				}
				if err := writeXyzBox(w, meta.Latitude, meta.Longitude); err != nil {
					return nil, err
				}
				if _, err := w.EndBox(); err != nil {
					return nil, err
				}
			}
			_, err := w.EndBox()
			return nil, err

		case mp4.BoxTypeMvhd():
			sawMvhd = true
			if _, err := w.StartBox(&h.BoxInfo); err != nil {
				return nil, err
			}
			box, _, err := h.ReadPayload()
			if err != nil {
				return nil, fmt.Errorf("reading mvhd payload: %w", err)
			}
			mvhd, ok := box.(*mp4.Mvhd)
			if !ok {
				return nil, errors.New("mvhd box has unexpected payload type")
			}
			if mvhd.GetVersion() == 0 {
				mvhd.CreationTimeV0 = uint32(creation)
				mvhd.ModificationTimeV0 = uint32(creation)
			} else {
				mvhd.CreationTimeV1 = creation
				mvhd.ModificationTimeV1 = creation
			}
			if _, err := mp4.Marshal(w, mvhd, h.BoxInfo.Context); err != nil {
				return nil, fmt.Errorf("writing mvhd: %w", err)
			}
			_, err = w.EndBox()
			return nil, err

		// Only reached for a udta directly under moov: children of copied
		// boxes (e.g. trak) are never expanded, so their udta stays intact.
		case mp4.BoxTypeUdta():
			sawUdta = true
			if _, err := w.StartBox(&h.BoxInfo); err != nil {
				return nil, err
			}
			if _, err := h.Expand(); err != nil {
				return nil, err
			}
			if err := writeXyzBox(w, meta.Latitude, meta.Longitude); err != nil {
				return nil, err
			}
			_, err := w.EndBox()
			return nil, err

		case xyzBoxType:
			// Dropped: replaced by the ©xyz written when the enclosing udta closes.
			return nil, nil

		default:
			return nil, w.CopyBox(r, &h.BoxInfo)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("rewriting MP4 boxes: %w", err)
	}
	if !sawMvhd {
		return nil, errors.New("no moov/mvhd box found: not a valid MP4")
	}

	return out.buf, nil
}

// writeXyzBox writes a ©xyz atom: 16-bit string length, 16-bit language code,
// then an ISO 6709 location string such as "+37.4200-122.0800/".
func writeXyzBox(w *mp4.Writer, lat, lon float64) error {
	loc := fmt.Sprintf("%+.4f%+.4f/", lat, lon)
	payload := make([]byte, 4+len(loc))
	binary.BigEndian.PutUint16(payload[0:2], uint16(len(loc)))
	// 0x15C7 is the packed ISO 639-2 code for "und" (undetermined language).
	binary.BigEndian.PutUint16(payload[2:4], 0x15C7)
	copy(payload[4:], loc)

	if _, err := w.StartBox(&mp4.BoxInfo{Type: xyzBoxType}); err != nil {
		return err
	}
	if _, err := w.Write(payload); err != nil {
		return err
	}
	_, err := w.EndBox()
	return err
}

// memWriteSeeker is an in-memory io.WriteSeeker: mp4.Writer needs to seek
// back and patch box sizes, which bytes.Buffer cannot do.
type memWriteSeeker struct {
	buf []byte
	pos int64
}

func (m *memWriteSeeker) Write(p []byte) (int, error) {
	if grow := m.pos + int64(len(p)) - int64(len(m.buf)); grow > 0 {
		m.buf = append(m.buf, make([]byte, grow)...)
	}
	copy(m.buf[m.pos:], p)
	m.pos += int64(len(p))
	return len(p), nil
}

func (m *memWriteSeeker) Seek(offset int64, whence int) (int64, error) {
	var pos int64
	switch whence {
	case io.SeekStart:
		pos = offset
	case io.SeekCurrent:
		pos = m.pos + offset
	case io.SeekEnd:
		pos = int64(len(m.buf)) + offset
	default:
		return 0, fmt.Errorf("invalid whence %d", whence)
	}
	if pos < 0 {
		return 0, errors.New("negative seek position")
	}
	m.pos = pos
	return pos, nil
}
