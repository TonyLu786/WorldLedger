// Package anvil converts canonical WorldLedger components into Minecraft Java
// Anvil region data.
//
// The NBT written here is vanilla file NBT: named tags, End-terminated
// compounds, and Java modified UTF-8. It is deliberately not the canonical NBT
// defined by worldledger.minecraft.java.chunk/v1, which is a hashing preimage
// and must never be handed to a vanilla reader.
package anvil

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/worldledger/worldledger-mc/internal/mcjava"
)

const maxNBTStringBytes = math.MaxUint16

// EncodeNamed writes one named root tag in vanilla file NBT form. Region chunk
// payloads and level.dat both use an empty root name.
func EncodeNamed(name string, value mcjava.NBTValue) ([]byte, error) {
	var buffer bytes.Buffer
	if err := writeNamedTag(&buffer, name, value); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func writeNamedTag(w *bytes.Buffer, name string, value mcjava.NBTValue) error {
	if value.Type == mcjava.TagEnd {
		return errors.New("End is not a standalone NBT value")
	}
	w.WriteByte(byte(value.Type))
	if err := writeModifiedUTF8(w, name); err != nil {
		return fmt.Errorf("tag name %q: %w", name, err)
	}
	return writePayload(w, value)
}

func writePayload(w *bytes.Buffer, value mcjava.NBTValue) error {
	switch value.Type {
	case mcjava.TagByte:
		w.WriteByte(byte(value.Byte))
	case mcjava.TagShort:
		writeU16(w, uint16(value.Short))
	case mcjava.TagInt:
		writeU32(w, uint32(value.Int))
	case mcjava.TagLong:
		writeU64(w, uint64(value.Long))
	case mcjava.TagFloat:
		writeU32(w, value.FloatBits)
	case mcjava.TagDouble:
		writeU64(w, value.DoubleBits)
	case mcjava.TagByteArray:
		if err := writeLength(w, len(value.ByteArray)); err != nil {
			return fmt.Errorf("byte array: %w", err)
		}
		w.Write(value.ByteArray)
	case mcjava.TagString:
		return writeModifiedUTF8(w, value.String)
	case mcjava.TagList:
		return writeList(w, value)
	case mcjava.TagCompound:
		for _, entry := range value.Compound {
			if err := writeNamedTag(w, entry.Name, entry.Value); err != nil {
				return err
			}
		}
		w.WriteByte(byte(mcjava.TagEnd))
	case mcjava.TagIntArray:
		if err := writeLength(w, len(value.IntArray)); err != nil {
			return fmt.Errorf("int array: %w", err)
		}
		for _, item := range value.IntArray {
			writeU32(w, uint32(item))
		}
	case mcjava.TagLongArray:
		if err := writeLength(w, len(value.LongArray)); err != nil {
			return fmt.Errorf("long array: %w", err)
		}
		for _, item := range value.LongArray {
			writeU64(w, uint64(item))
		}
	default:
		return fmt.Errorf("unsupported NBT tag type %d", value.Type)
	}
	return nil
}

func writeList(w *bytes.Buffer, value mcjava.NBTValue) error {
	if value.List == nil {
		return errors.New("list payload is required")
	}
	elementType := value.List.ElementType
	if len(value.List.Values) == 0 && elementType == mcjava.TagEnd {
		w.WriteByte(byte(mcjava.TagEnd))
		return writeLength(w, 0)
	}
	if elementType == mcjava.TagEnd {
		return errors.New("non-empty list cannot use End element type")
	}
	w.WriteByte(byte(elementType))
	if err := writeLength(w, len(value.List.Values)); err != nil {
		return fmt.Errorf("list: %w", err)
	}
	for index, item := range value.List.Values {
		if item.Type != elementType {
			return fmt.Errorf("list item %d has type %d; want %d", index, item.Type, elementType)
		}
		if err := writePayload(w, item); err != nil {
			return fmt.Errorf("list item %d: %w", index, err)
		}
	}
	return nil
}

func writeLength(w *bytes.Buffer, length int) error {
	if length < 0 || int64(length) > math.MaxInt32 {
		return fmt.Errorf("length %d does not fit in an NBT int", length)
	}
	writeU32(w, uint32(length))
	return nil
}

func writeU16(w *bytes.Buffer, value uint16) {
	var data [2]byte
	binary.BigEndian.PutUint16(data[:], value)
	w.Write(data[:])
}

func writeU32(w *bytes.Buffer, value uint32) {
	var data [4]byte
	binary.BigEndian.PutUint32(data[:], value)
	w.Write(data[:])
}

func writeU64(w *bytes.Buffer, value uint64) {
	var data [8]byte
	binary.BigEndian.PutUint64(data[:], value)
	w.Write(data[:])
}

// writeModifiedUTF8 encodes a Java modified UTF-8 string. It differs from
// standard UTF-8 in exactly two places, and both are reachable from server-
// supplied block entity text: U+0000 becomes two bytes, and any character
// outside the basic multilingual plane is written as its UTF-16 surrogate pair,
// six bytes rather than four.
func writeModifiedUTF8(w *bytes.Buffer, value string) error {
	if !utf8.ValidString(value) {
		return errors.New("string is not valid UTF-8")
	}
	encoded := modifiedUTF8(value)
	if len(encoded) > maxNBTStringBytes {
		return fmt.Errorf("string encodes to %d bytes; NBT allows %d", len(encoded), maxNBTStringBytes)
	}
	writeU16(w, uint16(len(encoded)))
	w.Write(encoded)
	return nil
}

func modifiedUTF8(value string) []byte {
	out := make([]byte, 0, len(value)+8)
	for _, r := range value {
		switch {
		case r == 0:
			out = append(out, 0xC0, 0x80)
		case r <= 0x7F:
			out = append(out, byte(r))
		case r <= 0x7FF:
			out = append(out, byte(0xC0|(r>>6)), byte(0x80|(r&0x3F)))
		case r <= 0xFFFF:
			out = appendThreeByte(out, uint16(r))
		default:
			high, low := utf16.EncodeRune(r)
			out = appendThreeByte(out, uint16(high))
			out = appendThreeByte(out, uint16(low))
		}
	}
	return out
}

func appendThreeByte(out []byte, value uint16) []byte {
	return append(out,
		byte(0xE0|(value>>12)),
		byte(0x80|((value>>6)&0x3F)),
		byte(0x80|(value&0x3F)),
	)
}
