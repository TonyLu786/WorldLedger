package mcjava

import (
	"errors"
	"fmt"
)

func DecodeNBT(data []byte) (NBTValue, error) {
	return DecodeNBTWithLimits(data, DefaultLimits())
}

func DecodeNBTWithLimits(data []byte, limits Limits) (NBTValue, error) {
	normalized, err := limits.normalized()
	if err != nil {
		return NBTValue{}, err
	}
	if len(data) > normalized.MaxNBTBytes {
		return NBTValue{}, fmt.Errorf("canonical NBT exceeds %d bytes", normalized.MaxNBTBytes)
	}
	r := &canonicalReader{data: data, limits: normalized}
	value, err := decodeNBTValue(r, 0)
	if err != nil {
		return NBTValue{}, err
	}
	if err := r.expectEnd(); err != nil {
		return NBTValue{}, err
	}
	return value, nil
}

func decodeNBTValue(r *canonicalReader, depth int) (NBTValue, error) {
	tag, err := r.readU8()
	if err != nil {
		return NBTValue{}, fmt.Errorf("tag type: %w", err)
	}
	tagType := TagType(tag)
	if tagType == TagEnd {
		return NBTValue{}, errors.New("End is not a standalone NBT value")
	}
	return decodeNBTPayload(r, tagType, depth)
}

func decodeNBTPayload(r *canonicalReader, tagType TagType, depth int) (NBTValue, error) {
	if depth > r.limits.MaxNBTDepth {
		return NBTValue{}, fmt.Errorf("NBT depth exceeds %d", r.limits.MaxNBTDepth)
	}
	value := NBTValue{Type: tagType}
	switch tagType {
	case TagByte:
		payload, err := r.readI8()
		if err != nil {
			return NBTValue{}, err
		}
		value.Byte = payload
	case TagShort:
		payload, err := r.readI16()
		if err != nil {
			return NBTValue{}, err
		}
		value.Short = payload
	case TagInt:
		payload, err := r.readI32()
		if err != nil {
			return NBTValue{}, err
		}
		value.Int = payload
	case TagLong:
		payload, err := r.readI64()
		if err != nil {
			return NBTValue{}, err
		}
		value.Long = payload
	case TagFloat:
		payload, err := r.readU32()
		if err != nil {
			return NBTValue{}, err
		}
		value.FloatBits = payload
	case TagDouble:
		payload, err := r.readU64()
		if err != nil {
			return NBTValue{}, err
		}
		value.DoubleBits = payload
	case TagByteArray:
		count, err := r.readCollectionCount(1, "byte array")
		if err != nil {
			return NBTValue{}, err
		}
		payload, err := r.take(count)
		if err != nil {
			return NBTValue{}, fmt.Errorf("byte array: %w", err)
		}
		value.ByteArray = append([]byte(nil), payload...)
	case TagString:
		payload, err := r.readString()
		if err != nil {
			return NBTValue{}, err
		}
		value.String = payload
	case TagList:
		list, err := decodeNBTList(r, depth)
		if err != nil {
			return NBTValue{}, err
		}
		value.List = list
	case TagCompound:
		entries, err := decodeNBTCompound(r, depth)
		if err != nil {
			return NBTValue{}, err
		}
		value.Compound = entries
	case TagIntArray:
		count, err := r.readCollectionCount(4, "int array")
		if err != nil {
			return NBTValue{}, err
		}
		items := make([]int32, count)
		for index := range items {
			item, err := r.readI32()
			if err != nil {
				return NBTValue{}, fmt.Errorf("int array item %d: %w", index, err)
			}
			items[index] = item
		}
		value.IntArray = items
	case TagLongArray:
		count, err := r.readCollectionCount(8, "long array")
		if err != nil {
			return NBTValue{}, err
		}
		items := make([]int64, count)
		for index := range items {
			item, err := r.readI64()
			if err != nil {
				return NBTValue{}, fmt.Errorf("long array item %d: %w", index, err)
			}
			items[index] = item
		}
		value.LongArray = items
	default:
		return NBTValue{}, fmt.Errorf("unsupported NBT tag type %d", tagType)
	}
	return value, nil
}

func decodeNBTList(r *canonicalReader, depth int) (*NBTList, error) {
	element, err := r.readU8()
	if err != nil {
		return nil, fmt.Errorf("list element type: %w", err)
	}
	elementType := TagType(element)
	if elementType > TagLongArray {
		return nil, fmt.Errorf("invalid list element type %d", elementType)
	}
	count, err := r.readCollectionCount(1, "list")
	if err != nil {
		return nil, err
	}
	if elementType == TagEnd && count != 0 {
		return nil, errors.New("non-empty list cannot use End element type")
	}
	values := make([]NBTValue, 0, count)
	for index := 0; index < count; index++ {
		item, err := decodeNBTPayload(r, elementType, depth+1)
		if err != nil {
			return nil, fmt.Errorf("list item %d: %w", index, err)
		}
		values = append(values, item)
	}
	return &NBTList{ElementType: elementType, Values: values}, nil
}

func decodeNBTCompound(r *canonicalReader, depth int) ([]NamedNBT, error) {
	count, err := r.readCollectionCount(1, "compound")
	if err != nil {
		return nil, err
	}
	entries := make([]NamedNBT, 0, count)
	for index := 0; index < count; index++ {
		key, err := r.readString()
		if err != nil {
			return nil, fmt.Errorf("compound key %d: %w", index, err)
		}
		if index > 0 {
			previous := entries[index-1].Name
			if key == previous {
				return nil, fmt.Errorf("duplicate compound key %q", key)
			}
			if key < previous {
				return nil, fmt.Errorf("compound key %q is not in canonical order", key)
			}
		}
		item, err := decodeNBTValue(r, depth+1)
		if err != nil {
			return nil, fmt.Errorf("compound key %q: %w", key, err)
		}
		entries = append(entries, NamedNBT{Name: key, Value: item})
	}
	return entries, nil
}

// readCollectionCount rejects a length that the remaining bytes cannot support,
// so an untrusted count can never drive a large speculative allocation.
func (r *canonicalReader) readCollectionCount(itemBytes int, what string) (int, error) {
	count, err := r.readU32()
	if err != nil {
		return 0, fmt.Errorf("%s length: %w", what, err)
	}
	if uint64(count) > uint64(r.limits.MaxCollectionItems) {
		return 0, fmt.Errorf("%s item count %d exceeds limit", what, count)
	}
	if uint64(count)*uint64(itemBytes) > uint64(r.remaining()) {
		return 0, fmt.Errorf("%s item count %d exceeds remaining bytes", what, count)
	}
	return int(count), nil
}
