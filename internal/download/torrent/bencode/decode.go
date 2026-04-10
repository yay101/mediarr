package bencode

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidFormat = errors.New("invalid bencode format")
	ErrUnexpectedEnd = errors.New("unexpected end of data")
)

type Value interface{}

type String string
type Int int64
type List []Value
type Dict map[string]Value

func Decode(data []byte) (Value, error) {
	r := &reader{data: data, pos: 0}
	return r.readValue()
}

type reader struct {
	data []byte
	pos  int
}

func (r *reader) remaining() int {
	return len(r.data) - r.pos
}

func (r *reader) readValue() (Value, error) {
	if r.pos >= len(r.data) {
		return nil, ErrUnexpectedEnd
	}

	switch r.data[r.pos] {
	case 'i':
		return r.readInt()
	case 'l':
		return r.readList()
	case 'd':
		return r.readDict()
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return r.readString()
	default:
		return nil, fmt.Errorf("%w: invalid byte '%c' at position %d", ErrInvalidFormat, r.data[r.pos], r.pos)
	}
}

func (r *reader) readInt() (Int, error) {
	r.pos++ // skip 'i'

	start := r.pos
	for r.pos < len(r.data) && r.data[r.pos] != 'e' {
		r.pos++
	}

	if r.pos >= len(r.data) {
		return 0, ErrUnexpectedEnd
	}

	var val int64
	_, err := fmt.Sscanf(string(r.data[start:r.pos]), "%d", &val)
	if err != nil {
		return 0, fmt.Errorf("invalid integer: %w", err)
	}

	r.pos++ // skip 'e'
	return Int(val), nil
}

func (r *reader) readList() (List, error) {
	r.pos++ // skip 'l'

	var list List
	for r.pos < len(r.data) && r.data[r.pos] != 'e' {
		val, err := r.readValue()
		if err != nil {
			return nil, err
		}
		list = append(list, val)
	}

	if r.pos >= len(r.data) {
		return nil, ErrUnexpectedEnd
	}

	r.pos++ // skip 'e'
	return list, nil
}

func (r *reader) readDict() (Dict, error) {
	r.pos++ // skip 'd'

	dict := make(Dict)
	for r.pos < len(r.data) && r.data[r.pos] != 'e' {
		// Read key
		keyBytes, err := r.readBytes()
		if err != nil {
			return nil, fmt.Errorf("dict key: %w", err)
		}

		// Read value
		val, err := r.readValue()
		if err != nil {
			return nil, fmt.Errorf("dict value for key %q: %w", string(keyBytes), err)
		}

		dict[string(keyBytes)] = val
	}

	if r.pos >= len(r.data) {
		return nil, ErrUnexpectedEnd
	}

	r.pos++ // skip 'e'
	return dict, nil
}

func (r *reader) readString() (String, error) {
	bytes, err := r.readBytes()
	if err != nil {
		return "", err
	}
	return String(bytes), nil
}

func (r *reader) readBytes() ([]byte, error) {
	if r.pos >= len(r.data) {
		return nil, ErrUnexpectedEnd
	}

	// Read length
	start := r.pos
	for r.pos < len(r.data) && r.data[r.pos] != ':' {
		r.pos++
	}

	if r.pos >= len(r.data) {
		return nil, ErrUnexpectedEnd
	}

	var length int
	_, err := fmt.Sscanf(string(r.data[start:r.pos]), "%d", &length)
	if err != nil {
		return nil, fmt.Errorf("invalid string length: %w", err)
	}

	r.pos++ // skip ':'

	if r.pos+length > len(r.data) {
		return nil, ErrUnexpectedEnd
	}

	result := r.data[r.pos : r.pos+length]
	r.pos += length
	return result, nil
}

func Encode(v Value) ([]byte, error) {
	switch val := v.(type) {
	case string:
		return encodeString(val), nil
	case []byte:
		return encodeBytes(val), nil
	case int64:
		return encodeInt(val), nil
	case int:
		return encodeInt(int64(val)), nil
	case Int:
		return encodeInt(int64(val)), nil
	case String:
		return encodeString(string(val)), nil
	case List:
		return encodeList(val), nil
	case Dict:
		return encodeDict(val), nil
	case map[string]Value:
		d := make(Dict)
		for k, v := range val {
			d[k] = v
		}
		return encodeDict(d), nil
	default:
		return nil, fmt.Errorf("unsupported type: %T", v)
	}
}

func encodeString(s string) []byte {
	data := fmt.Sprintf("%d:%s", len(s), s)
	return []byte(data)
}

func encodeBytes(b []byte) []byte {
	return append(append([]byte{}, []byte(fmt.Sprintf("%d:", len(b)))...), b...)
}

func encodeInt(i int64) []byte {
	return []byte(fmt.Sprintf("i%de", i))
}

func encodeList(l List) []byte {
	result := []byte("l")
	for _, v := range l {
		encoded, err := Encode(v)
		if err != nil {
			return nil
		}
		result = append(result, encoded...)
	}
	result = append(result, 'e')
	return result
}

func encodeDict(d Dict) []byte {
	result := []byte("d")
	for _, k := range sortedKeys(d) {
		result = append(result, encodeString(k)...)
		encoded, err := Encode(d[k])
		if err != nil {
			return nil
		}
		result = append(result, encoded...)
	}
	result = append(result, 'e')
	return result
}

func sortedKeys(d Dict) []string {
	keys := make([]string, 0, len(d))
	for k := range d {
		keys = append(keys, k)
	}
	// Simple string sort
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] > keys[j] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

func DecodeInfoDict(data []byte) (Dict, error) {
	val, err := Decode(data)
	if err != nil {
		return nil, err
	}

	dict, ok := val.(Dict)
	if !ok {
		return nil, fmt.Errorf("expected dict at root")
	}

	info, ok := dict["info"]
	if !ok {
		return nil, fmt.Errorf("missing info dict")
	}

	infoDict, ok := info.(Dict)
	if !ok {
		return nil, fmt.Errorf("expected dict for info")
	}

	return infoDict, nil
}
