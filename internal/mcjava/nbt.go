package mcjava

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

type TagType uint8

const (
	TagEnd       TagType = 0
	TagByte      TagType = 1
	TagShort     TagType = 2
	TagInt       TagType = 3
	TagLong      TagType = 4
	TagFloat     TagType = 5
	TagDouble    TagType = 6
	TagByteArray TagType = 7
	TagString    TagType = 8
	TagList      TagType = 9
	TagCompound  TagType = 10
	TagIntArray  TagType = 11
	TagLongArray TagType = 12
)

type NBTList struct {
	ElementType TagType
	Values      []NBTValue
}

type NamedNBT struct {
	Name  string
	Value NBTValue
}

type NBTValue struct {
	Type       TagType
	Byte       int8
	Short      int16
	Int        int32
	Long       int64
	FloatBits  uint32
	DoubleBits uint64
	ByteArray  []byte
	String     string
	List       *NBTList
	Compound   []NamedNBT
	IntArray   []int32
	LongArray  []int64
}

func EncodeNBT(value NBTValue) ([]byte, error) {
	return EncodeNBTWithLimits(value, DefaultLimits())
}

func EncodeNBTWithLimits(value NBTValue, limits Limits) ([]byte, error) {
	limits, err := limits.normalized()
	if err != nil {
		return nil, err
	}
	w := newCanonicalWriter(limits.MaxNBTBytes, limits.MaxStringBytes)
	if err := encodeNBTValue(w, value, limits, 0); err != nil {
		return nil, err
	}
	return w.bytes(), nil
}

func encodeNBTValue(w *canonicalWriter, value NBTValue, limits Limits, depth int) error {
	if value.Type == TagEnd {
		return errors.New("End is not a standalone NBT value")
	}
	if err := w.writeU8(uint8(value.Type)); err != nil {
		return err
	}
	return encodeNBTPayload(w, value, limits, depth)
}

func encodeNBTPayload(w *canonicalWriter, value NBTValue, limits Limits, depth int) error {
	if depth > limits.MaxNBTDepth {
		return fmt.Errorf("NBT depth exceeds %d", limits.MaxNBTDepth)
	}
	switch value.Type {
	case TagByte:
		return w.writeI8(value.Byte)
	case TagShort:
		return w.writeI16(value.Short)
	case TagInt:
		return w.writeI32(value.Int)
	case TagLong:
		return w.writeI64(value.Long)
	case TagFloat:
		return w.writeU32(value.FloatBits)
	case TagDouble:
		return w.writeU64(value.DoubleBits)
	case TagByteArray:
		if err := validateCollectionLength(len(value.ByteArray), limits); err != nil {
			return fmt.Errorf("byte array: %w", err)
		}
		if err := w.writeU32(uint32(len(value.ByteArray))); err != nil {
			return err
		}
		return w.write(value.ByteArray)
	case TagString:
		return w.writeString(value.String)
	case TagList:
		if value.List == nil {
			return errors.New("list payload is required")
		}
		if err := validateCollectionLength(len(value.List.Values), limits); err != nil {
			return fmt.Errorf("list: %w", err)
		}
		if value.List.ElementType == TagEnd && len(value.List.Values) != 0 {
			return errors.New("non-empty list cannot use End element type")
		}
		if value.List.ElementType > TagLongArray {
			return fmt.Errorf("invalid list element type %d", value.List.ElementType)
		}
		if err := w.writeU8(uint8(value.List.ElementType)); err != nil {
			return err
		}
		if err := w.writeU32(uint32(len(value.List.Values))); err != nil {
			return err
		}
		for index, item := range value.List.Values {
			if item.Type != value.List.ElementType {
				return fmt.Errorf("list item %d has type %d; want %d", index, item.Type, value.List.ElementType)
			}
			if err := encodeNBTPayload(w, item, limits, depth+1); err != nil {
				return fmt.Errorf("list item %d: %w", index, err)
			}
		}
		return nil
	case TagCompound:
		if err := validateCollectionLength(len(value.Compound), limits); err != nil {
			return fmt.Errorf("compound: %w", err)
		}
		entries := append([]NamedNBT(nil), value.Compound...)
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Name < entries[j].Name
		})
		if err := w.writeU32(uint32(len(entries))); err != nil {
			return err
		}
		for index, entry := range entries {
			if index > 0 && entries[index-1].Name == entry.Name {
				return fmt.Errorf("duplicate compound key %q", entry.Name)
			}
			if err := w.writeString(entry.Name); err != nil {
				return fmt.Errorf("compound key %q: %w", entry.Name, err)
			}
			if err := encodeNBTValue(w, entry.Value, limits, depth+1); err != nil {
				return fmt.Errorf("compound key %q: %w", entry.Name, err)
			}
		}
		return nil
	case TagIntArray:
		if err := validateCollectionLength(len(value.IntArray), limits); err != nil {
			return fmt.Errorf("int array: %w", err)
		}
		if err := w.writeU32(uint32(len(value.IntArray))); err != nil {
			return err
		}
		for _, item := range value.IntArray {
			if err := w.writeI32(item); err != nil {
				return err
			}
		}
		return nil
	case TagLongArray:
		if err := validateCollectionLength(len(value.LongArray), limits); err != nil {
			return fmt.Errorf("long array: %w", err)
		}
		if err := w.writeU32(uint32(len(value.LongArray))); err != nil {
			return err
		}
		for _, item := range value.LongArray {
			if err := w.writeI64(item); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported NBT tag type %d", value.Type)
	}
}

func validateCollectionLength(length int, limits Limits) error {
	if length < 0 || length > limits.MaxCollectionItems || uint64(length) > math.MaxUint32 {
		return fmt.Errorf("item count %d exceeds limit", length)
	}
	return nil
}
