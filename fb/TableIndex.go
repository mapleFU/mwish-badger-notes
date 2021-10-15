// Code generated by the FlatBuffers compiler. DO NOT EDIT.

package fb

import (
	flatbuffers "github.com/google/flatbuffers/go"
)

// 这块东西是用 flat buffer 做的.
type TableIndex struct {
	_tab flatbuffers.Table
}

func GetRootAsTableIndex(buf []byte, offset flatbuffers.UOffsetT) *TableIndex {
	n := flatbuffers.GetUOffsetT(buf[offset:])
	x := &TableIndex{}
	x.Init(buf, n+offset)
	return x
}

func (rcv *TableIndex) Init(buf []byte, i flatbuffers.UOffsetT) {
	rcv._tab.Bytes = buf
	rcv._tab.Pos = i
}

func (rcv *TableIndex) Table() flatbuffers.Table {
	return rcv._tab
}

func (rcv *TableIndex) Offsets(obj *BlockOffset, j int) bool {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(4))
	if o != 0 {
		x := rcv._tab.Vector(o)
		x += flatbuffers.UOffsetT(j) * 4
		x = rcv._tab.Indirect(x)
		obj.Init(rcv._tab.Bytes, x)
		return true
	}
	return false
}

func (rcv *TableIndex) OffsetsLength() int {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(4))
	if o != 0 {
		return rcv._tab.VectorLen(o)
	}
	return 0
}

func (rcv *TableIndex) BloomFilter(j int) byte {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(6))
	if o != 0 {
		a := rcv._tab.Vector(o)
		return rcv._tab.GetByte(a + flatbuffers.UOffsetT(j*1))
	}
	return 0
}

func (rcv *TableIndex) BloomFilterLength() int {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(6))
	if o != 0 {
		return rcv._tab.VectorLen(o)
	}
	return 0
}

func (rcv *TableIndex) BloomFilterBytes() []byte {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(6))
	if o != 0 {
		return rcv._tab.ByteVector(o + rcv._tab.Pos)
	}
	return nil
}

func (rcv *TableIndex) MutateBloomFilter(j int, n byte) bool {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(6))
	if o != 0 {
		a := rcv._tab.Vector(o)
		return rcv._tab.MutateByte(a+flatbuffers.UOffsetT(j*1), n)
	}
	return false
}

func (rcv *TableIndex) MaxVersion() uint64 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(8))
	if o != 0 {
		return rcv._tab.GetUint64(o + rcv._tab.Pos)
	}
	return 0
}

func (rcv *TableIndex) MutateMaxVersion(n uint64) bool {
	return rcv._tab.MutateUint64Slot(8, n)
}

func (rcv *TableIndex) KeyCount() uint32 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(10))
	if o != 0 {
		return rcv._tab.GetUint32(o + rcv._tab.Pos)
	}
	return 0
}

func (rcv *TableIndex) MutateKeyCount(n uint32) bool {
	return rcv._tab.MutateUint32Slot(10, n)
}

func (rcv *TableIndex) UncompressedSize() uint32 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(12))
	if o != 0 {
		return rcv._tab.GetUint32(o + rcv._tab.Pos)
	}
	return 0
}

func (rcv *TableIndex) MutateUncompressedSize(n uint32) bool {
	return rcv._tab.MutateUint32Slot(12, n)
}

func (rcv *TableIndex) OnDiskSize() uint32 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(14))
	if o != 0 {
		return rcv._tab.GetUint32(o + rcv._tab.Pos)
	}
	return 0
}

func (rcv *TableIndex) MutateOnDiskSize(n uint32) bool {
	return rcv._tab.MutateUint32Slot(14, n)
}

func (rcv *TableIndex) StaleDataSize() uint32 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(16))
	if o != 0 {
		return rcv._tab.GetUint32(o + rcv._tab.Pos)
	}
	return 0
}

func (rcv *TableIndex) MutateStaleDataSize(n uint32) bool {
	return rcv._tab.MutateUint32Slot(16, n)
}

func TableIndexStart(builder *flatbuffers.Builder) {
	builder.StartObject(7)
}
func TableIndexAddOffsets(builder *flatbuffers.Builder, offsets flatbuffers.UOffsetT) {
	builder.PrependUOffsetTSlot(0, flatbuffers.UOffsetT(offsets), 0)
}
func TableIndexStartOffsetsVector(builder *flatbuffers.Builder, numElems int) flatbuffers.UOffsetT {
	return builder.StartVector(4, numElems, 4)
}
func TableIndexAddBloomFilter(builder *flatbuffers.Builder, bloomFilter flatbuffers.UOffsetT) {
	builder.PrependUOffsetTSlot(1, flatbuffers.UOffsetT(bloomFilter), 0)
}
func TableIndexStartBloomFilterVector(builder *flatbuffers.Builder, numElems int) flatbuffers.UOffsetT {
	return builder.StartVector(1, numElems, 1)
}
func TableIndexAddMaxVersion(builder *flatbuffers.Builder, maxVersion uint64) {
	builder.PrependUint64Slot(2, maxVersion, 0)
}
func TableIndexAddKeyCount(builder *flatbuffers.Builder, keyCount uint32) {
	builder.PrependUint32Slot(3, keyCount, 0)
}
func TableIndexAddUncompressedSize(builder *flatbuffers.Builder, uncompressedSize uint32) {
	builder.PrependUint32Slot(4, uncompressedSize, 0)
}
func TableIndexAddOnDiskSize(builder *flatbuffers.Builder, onDiskSize uint32) {
	builder.PrependUint32Slot(5, onDiskSize, 0)
}
func TableIndexAddStaleDataSize(builder *flatbuffers.Builder, staleDataSize uint32) {
	builder.PrependUint32Slot(6, staleDataSize, 0)
}
func TableIndexEnd(builder *flatbuffers.Builder) flatbuffers.UOffsetT {
	return builder.EndObject()
}
