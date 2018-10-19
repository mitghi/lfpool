/**
* MIT License
*
* Copyright (c) 2017 Mike Taghavi <mitghi@me.com>
*
* Permission is hereby granted, free of charge, to any person obtaining a copy
* of this software and associated documentation files (the "Software"), to deal
* in the Software without restriction, including without limitation the rights
* to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
* copies of the Software, and to permit persons to whom the Software is
* furnished to do so, subject to the following conditions:
*
* The above copyright notice and this permission notice shall be included in all
* copies or substantial portions of the Software.
*
* THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
* IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
* FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
* AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
* LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
* OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
* SOFTWARE.
**/

package lfpool

import (
	"errors"
	"sync/atomic"
	"unsafe"
)

/**
* TODO
* . implement LRU to prune buckets
* . finish pointer marking
* . add an option for min/max supported allocation size
**/

const (
	lBlkMax   = (0x100 << 0x13)
	lBlkMask  = 0x3F
	lBlkShift = 0x03
	cMax32    = 0xFFFFFFFF
	cMin32    = 0x00000000
	lMax32    = 0x00000020
)

const (
	calthld    = 42000
	percentile = 0.95
	minSize    = 64
	maxSize    = 2097152
	cLFSIZE    = 64
)

var (
	LPNotSupported error = errors.New("lfpool: op not supported.")
)

var (
	mDeBruijnBitPosition [32]int = [32]int{
		0, 9, 1, 10, 13, 21, 2, 29, 11, 14, 16, 18, 22, 25, 3, 30,
		8, 12, 20, 28, 15, 17, 24, 7, 19, 27, 23, 6, 26, 5, 4, 31,
	}
	blocks blktable = blktable{
		2, 4, 8,
		16, 32, 64,
		128, 256, 512,
		1024, 2048, 4096, 8192,
		16384, 32768, 65536, 131072,
		262144, 524288, 1048576, 2097152,
		4194304, 8388608, 16777216,
		33554432, 67108864, 134217728,
		268435456, 536870912, 1073741824,
		2147483648, 4294967296,
	}
)

type BuffPool struct {
	slots [32]bpnode
	stats *Stats
}

type Stats struct {
	blocks [32]stat
	defbs  uint64
	max    uint64
	safe   uint32
	auto   uint32
}

type stat struct {
	allocs   uint64
	rels     uint64
	deallocs uint64
	min      uint64
	max      uint64
}

type blktable []int

type markedPtr struct {
	next unsafe.Pointer // *lfslice
	mark uint32
}

type lfslice struct {
	data  [cLFSIZE]unsafe.Pointer
	count uint32
	next  unsafe.Pointer // *markedPtr
}

type bpnode struct {
	entry unsafe.Pointer // *lfslice
	flag  uint32
}

type qnode struct {
	next unsafe.Pointer // *qnode
	data *[]byte
}

type lbstat struct {
	allocs uint64
	size   uint64
}

func (blkt blktable) bin(num int) int {
	return int(blkt.lgb2(uint32(num)))
}

func (blkt blktable) size(num int) int {
	bin := int(blkt.bin(num))
	return blkt[bin]
}

func (blkt blktable) nextPow2(num uint32) uint32 {
	num--
	num |= num >> 1
	num |= num >> 2
	num |= num >> 4
	num |= num >> 8
	num |= num >> 16
	num++
	return num
}

func (blkt blktable) lgb2(num uint32) int {
	npw := blkt.nextPow2(num) - 1
	return mDeBruijnBitPosition[int(uint32(npw*(uint32)(0x07C4ACDD))>>27)]
}

func NewBuffPool() (bp *BuffPool) {
	bp = &BuffPool{}
	bp.stats = nil
	for i, _ := range bp.slots {
		bp.slots[i].entry = unsafe.Pointer(newlfslice())
	}
	return bp
}

func WithStats() (bp *BuffPool) {
	bp = &BuffPool{
		stats: &Stats{},
	}
	for i, _ := range bp.slots {
		bp.slots[i].entry = unsafe.Pointer(newlfslice())
	}
	return bp
}

func (bpn *bpnode) ldEntry() unsafe.Pointer {
	return atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&bpn.entry)))
}

func (bpn *bpnode) detach() *lfslice {
	var hptr unsafe.Pointer
	for {
		hptr = atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&bpn.entry)))
		if atomic.CompareAndSwapPointer(
			(*unsafe.Pointer)(unsafe.Pointer(&bpn.entry)),
			(unsafe.Pointer)(unsafe.Pointer(bpn.entry)),
			(unsafe.Pointer)(unsafe.Pointer(newlfslice())),
		) {
			break
		}
	}

	return (*lfslice)(hptr)
}

func (bp *BuffPool) cleanUp(head *lfslice) {
	// TODO
	// . do cleanup, sorting, ....

	// runtime.LockOSThread()
	// defer runtime.UnlockOSThread()
	// for curr := head.Next(); (*lfslice)(curr) != nil; an = ((*markedPtr)((*lfslice)(curr).getNextptr()).Next()) {
	// }
}

func (bp *BuffPool) Get(chunks ...int) []byte {
	cl := len(chunks)
	switch cl {
	case 1:
		chunk := bp.getChunk(chunks[0])
		chunk = chunk[:cap(chunk)]
		return chunk
	case 2:
		if chunks[0] > chunks[1] {
			panic("len>cap")
		}
		chunk := bp.getChunk(chunks[1])
		chunk = chunk[:chunks[0]]
		return chunk
	default:
		chunk := bp.getChunk(0) // 64
		chunk = chunk[:cap(chunk)]
		return chunk
	}
}

func (bp *BuffPool) AutoGet() ([]byte, error) {
	// if atomic.Load
	if bp.stats != nil && atomic.LoadUint32(&bp.stats.auto) != 0 {
		chunk := bp.Get(int(atomic.LoadUint64(&bp.stats.defbs)))
		return chunk, nil
	}
	return nil, LPNotSupported
}

func (bp *BuffPool) GetAutoBuffer() (*Buffer, error) {
	chunk, err := bp.AutoGet()
	if err != nil {
		return nil, err
	}
	b := &Buffer{chunk, bp, true}
	b.Reset()
	return b, nil
}

func (bp *BuffPool) GetBuffer(chunks ...int) *Buffer {
	b := &Buffer{bp.Get(chunks...), bp, false}
	b.Reset()
	return b
}

func (bp *BuffPool) Release(chunk []byte) {
	bp.releaseChunk(chunk)
}

func (bp *BuffPool) ReleaseBuffer(b *Buffer) {
	if b != nil {
		bp.releaseChunk(b.Data)
	}
}

func (bp *BuffPool) AutoReleaseBuffer(b *Buffer) {
	if b != nil && b.auto {
		bp.AutoRelease(b.Data)
	}
}

func (bp *BuffPool) AutoRelease(chunk []byte) {
	var (
		slot     bpnode
		np       int
		index    int
		capacity int
	)
	capacity = cap(chunk)
	if capacity&0xffffffc0 == 0 || capacity > 0x02000000 {
		return
	} else {
		index = blocks.lgb2(uint32(capacity))
	}
	if atomic.AddUint64(&bp.stats.blocks[index].rels, 1) > calthld {
		bp.stats.adapt()
	}
	nc := atomic.LoadUint64(&bp.stats.max)
	if capacity <= int(nc) {
		np = blocks[index]
		if capacity < int(np) {
			ctmp := make([]byte, np)
			copy(ctmp, chunk)
			chunk = ctmp
		}
		slotptr := bp.ldSlot(index, unsafe.Sizeof(slot))
		entry := (*bpnode)(slotptr).ldEntry()
		(*lfslice)(entry).Insert(chunk)
	}
}

func (bp *BuffPool) getChunk(chunk int) []byte {
	var (
		slot     bpnode
		ret      []byte
		capacity int
		index    int
	)
	if chunk&0xffffffc0 == 0 {
		index = blocks.lgb2(0x3f)
	} else if uint32(chunk) >= 0x02000000 {
		index = blocks.lgb2(0x01ffffff)
	} else {
		index = blocks.lgb2(uint32(chunk - 1))
	}
	capacity = blocks[index]
	slotptr := bp.ldSlot(index, unsafe.Sizeof(slot))
	entry := (*bpnode)(slotptr).ldEntry()
	ret = (*lfslice)(entry).Get()
	if ret == nil {
		if bp.stats != nil {
			atomic.AddUint64(&bp.stats.blocks[index].allocs, 1)
		}
		b := make([]byte, capacity)
		return b
	}
	return ret
}

func (bp *BuffPool) releaseChunk(chunk []byte) {
	var (
		slot     bpnode
		np       int
		index    int
		capacity int
	)
	capacity = cap(chunk)
	if capacity&0xffffffc0 == 0 || capacity > 0x02000000 {
		// drop ( until min/max allc. range is added )
		return
	} else {
		index = blocks.lgb2(uint32(capacity))
	}
	np = blocks[index]
	if capacity < int(np) {
		ctmp := make([]byte, np)
		copy(ctmp, chunk)
		chunk = ctmp
	}
	slotptr := bp.ldSlot(index, unsafe.Sizeof(slot))
	entry := (*bpnode)(slotptr).ldEntry()
	(*lfslice)(entry).Insert(chunk)
	if bp.stats != nil {
		atomic.AddUint64(&bp.stats.blocks[index].rels, 1)
	}
}

func (bp *BuffPool) ldSlot(index int, size uintptr) unsafe.Pointer {
	var (
		nptr    unsafe.Pointer  = unsafe.Pointer(&bp.slots)
		bin     *unsafe.Pointer = (*unsafe.Pointer)(unsafe.Pointer(uintptr(nptr) + size*uintptr(index)))
		slotptr unsafe.Pointer
	)
	slotptr = atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&bin)))
	return slotptr
}
