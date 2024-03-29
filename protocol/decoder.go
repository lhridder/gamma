package protocol

import (
	"bytes"
	"errors"
	"fmt"
	"io"
)

const (
	// header is the header of compressed 'batches' from Minecraft.
	header = 0xfe
	// maximumInBatch is the maximum amount of packets that may be found in a batch. If a compressed batch has
	// more than this amount, decoding will fail.
	maximumInBatch = 512 + 256
)

type Decoder struct {
	// r holds the io.Reader that packets are read from if the reader does not implement packetReader. When
	// this is the case, the buf field has a non-zero length.
	r           io.Reader
	buf         []byte
	compression Compression
}

// NewDecoder returns a new decoder decoding data from the io.Reader passed. One read call from the reader is
// assumed to consume an entire packet.
func NewDecoder(reader io.Reader) *Decoder {
	return &Decoder{
		r:   reader,
		buf: make([]byte, 1024*1024*3),
	}
}

// Decode decodes one 'packet' from the io.Reader passed in NewDecoder(), producing a slice of packets that it
// held and an error if not successful.
func (decoder *Decoder) Decode() (packets [][]byte, err error) {
	n, err := decoder.r.Read(decoder.buf)
	if err != nil {
		return nil, fmt.Errorf("error reading batch from reader: %v", err)
	}
	data := decoder.buf[:n]

	if len(data) == 0 {
		return nil, nil
	}
	if data[0] != header {
		return nil, fmt.Errorf("error reading packet: invalid packet header %x: expected %x", data[0], header)
	}
	data = data[1:]

	if decoder.compression != nil {
		data, err = decoder.compression.Decompress(data)
		if err != nil {
			return nil, fmt.Errorf("error decompressing packet: %v", err)
		}
	}
	b := bytes.NewBuffer(data)
	for b.Len() != 0 {
		var length uint32
		if err := Varuint32(b, &length); err != nil {
			return nil, fmt.Errorf("error reading packet length: %v", err)
		}
		packets = append(packets, b.Next(int(length)))
	}
	if len(packets) > maximumInBatch {
		return nil, fmt.Errorf("number of packets %v in compressed batch exceeds %v", len(packets), maximumInBatch)
	}
	return packets, nil
}

func Varuint32(src io.ByteReader, x *uint32) error {
	var v uint32
	for i := uint(0); i < 35; i += 7 {
		b, err := src.ReadByte()
		if err != nil {
			return err
		}
		v |= uint32(b&0x7f) << i
		if b&0x80 == 0 {
			*x = v
			return nil
		}
	}
	return errors.New("varuint32 did not terminate after 5 bytes")
}

// EnableCompression enables compression for the Decoder.
func (decoder *Decoder) EnableCompression(compression Compression) {
	decoder.compression = compression
}
