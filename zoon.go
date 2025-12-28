package zoon

import (
	"bytes"
	"errors"
	"io"
)

// Encoder writes ZOON format to an output stream.
type Encoder struct {
	w io.Writer
}

// NewEncoder returns a new encoder that writes to w.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w}
}

// Encode writes the encoding of v to the stream.
func (e *Encoder) Encode(v any) error {
	return e.encode(v)
}

// Decoder reads ZOON values from an input stream.
type Decoder struct {
	r io.Reader
}

// NewDecoder returns a new decoder that reads from r.
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r: r}
}

// Decode reads the next JSON-encoded value from its input and stores it in the value pointed to by v.
func (d *Decoder) Decode(v any) error {
	return d.decode(v)
}

// Marshal returns the ZOON encoding of v.
func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := NewEncoder(&buf).Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Unmarshal parses the ZOON-encoded data and stores the result in the value pointed to by v.
func Unmarshal(data []byte, v any) error {
	return NewDecoder(bytes.NewReader(data)).Decode(v)
}

var (
	ErrUnsupportedType = errors.New("zoon: unsupported type")
	ErrInvalidFormat   = errors.New("zoon: invalid format")
)
