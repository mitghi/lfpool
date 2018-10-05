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
	"io"
	"runtime"
	"sync/atomic"
	"unsafe"
)

/**
* TODO
* . implement LRU to prune buckets
* . test `detach(....)`
* . improve `detach(....)`
* . finish pointer marking
* . add an option for min/max supported allocation size
*
* IDEAS
* . use lfqueue as lfslice container
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

type (
	BuffPool struct {
		slots [32]bpnode
		stats *Stats
	}

	Buffer struct {
		Data []byte
		mp   *BuffPool
		auto bool
	}

	Stats struct {
		blocks [32]stat
		defbs  uint64
		max    uint64
		safe   uint32
		auto   uint32
	}

	stat struct {
		allocs   uint64
		rels     uint64
		deallocs uint64
		min      uint64
		max      uint64
	}

	blktable []int

	markedPtr struct {
		next unsafe.Pointer // *lfslice
		mark uint32
	}

	lfslice struct {
		data  [cLFSIZE]unsafe.Pointer
		count uint32
		next  unsafe.Pointer // *markedPtr
	}

	bpnode struct {
		entry unsafe.Pointer // *lfslice
		flag  uint32
	}

	qentry struct {
		head  unsafe.Pointer // *qnode
		tail  unsafe.Pointer // *qnode
		count uint64
	}

	lfqueue struct {
		entry *qentry
	}

	qnode struct {
		next unsafe.Pointer // *qnode
		data *[]byte
	}

	lbstat struct {
		allocs uint64
		size   uint64
	}
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

func (self blktable) bin(num int) int {
	return int(self.lgb2(uint32(num)))
}

func (self blktable) size(num int) int {
	bin := int(self.bin(num))
	return self[bin]
}

func (self blktable) nextPow2(num uint32) uint32 {
	num--
	num |= num >> 1
	num |= num >> 2
	num |= num >> 4
	num |= num >> 8
	num |= num >> 16
	num++
	return num
}

func (self blktable) lgb2(num uint32) int {
	npw := self.nextPow2(num) - 1
	return mDeBruijnBitPosition[int(uint32(npw*(uint32)(0x07C4ACDD))>>27)]
}

func NewBuffPool() *BuffPool {
	ret := &BuffPool{}
	ret.stats = nil
	for i, _ := range ret.slots {
		ret.slots[i].entry = unsafe.Pointer(newlfslice())
	}
	return ret
}

func WithStats() *BuffPool {
	ret := &BuffPool{}
	s := Stats{}
	ret.stats = &s
	for i, _ := range ret.slots {
		ret.slots[i].entry = unsafe.Pointer(newlfslice())
	}
	return ret
}

func (self *bpnode) ldEntry() unsafe.Pointer {
	return atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&self.entry)))
}

func (self *bpnode) detach() *lfslice {
	var hptr unsafe.Pointer
	for {
		hptr = atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&self.entry)))
		if atomic.CompareAndSwapPointer(
			(*unsafe.Pointer)(unsafe.Pointer(&self.entry)),
			(unsafe.Pointer)(unsafe.Pointer(self.entry)),
			(unsafe.Pointer)(unsafe.Pointer(newlfslice())),
		) {
			break
		}
	}

	return (*lfslice)(hptr)
}

func (self *BuffPool) cleanUp(head *lfslice) {
	// TODO
	//  do cleanup, sorting, ....

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// for curr := head.Next(); (*lfslice)(curr) != nil; an = ((*markedPtr)((*lfslice)(curr).getNextptr()).Next()) {
	// }
}

func (self *BuffPool) Get(chunks ...int) []byte {
	cl := len(chunks)
	switch cl {
	case 1:
		chunk := self.getChunk(chunks[0])
		chunk = chunk[:cap(chunk)]
		return chunk
	case 2:
		if chunks[0] > chunks[1] {
			panic("len>cap")
		}
		chunk := self.getChunk(chunks[1])
		chunk = chunk[:chunks[0]]
		return chunk
	default:
		chunk := self.getChunk(0) // 64
		chunk = chunk[:cap(chunk)]
		return chunk
	}
}

func (self *BuffPool) AutoGet() ([]byte, error) {
	// if atomic.Load
	if self.stats != nil && atomic.LoadUint32(&self.stats.auto) != 0 {
		chunk := self.Get(int(atomic.LoadUint64(&self.stats.defbs)))
		return chunk, nil
	}
	return nil, LPNotSupported
}

func (self *BuffPool) GetAutoBuffer() (*Buffer, error) {
	chunk, err := self.AutoGet()
	if err != nil {
		return nil, err
	}
	b := &Buffer{chunk, self, true}
	b.Reset()
	return b, nil
}

func (self *BuffPool) GetBuffer(chunks ...int) *Buffer {
	b := &Buffer{self.Get(chunks...), self, false}
	b.Reset()
	return b
}

func (self *BuffPool) Release(chunk []byte) {
	self.releaseChunk(chunk)
}

func (self *BuffPool) ReleaseBuffer(b *Buffer) {
	if b != nil {
		self.releaseChunk(b.Data)
	}
}

func (self *BuffPool) AutoReleaseBuffer(b *Buffer) {
	if b != nil && b.auto {
		self.AutoRelease(b.Data)
	}
}

func (self *BuffPool) AutoRelease(chunk []byte) {
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
	if atomic.AddUint64(&self.stats.blocks[index].rels, 1) > calthld {
		self.stats.adapt()
	}
	nc := atomic.LoadUint64(&self.stats.max)
	if capacity <= int(nc) {
		np = blocks[index]
		if capacity < int(np) {
			ctmp := make([]byte, np)
			copy(ctmp, chunk)
			chunk = ctmp
		}
		slotptr := self.ldSlot(index, unsafe.Sizeof(slot))
		entry := (*bpnode)(slotptr).ldEntry()
		(*lfslice)(entry).Insert(chunk)
	}
}

func (self *BuffPool) getChunk(chunk int) []byte {
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
	// TODO
	//--------
	slotptr := self.ldSlot(index, unsafe.Sizeof(slot))
	entry := (*bpnode)(slotptr).ldEntry()
	ret = (*lfslice)(entry).Get()
	//--------
	// slot = self.slots[index]
	// ret = slot.entry.Get()
	if ret == nil {
		if self.stats != nil {
			atomic.AddUint64(&self.stats.blocks[index].allocs, 1)
		}
		b := make([]byte, capacity)
		return b
	}
	return ret
}

func (self *BuffPool) releaseChunk(chunk []byte) {
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
	// TODO
	//--------
	slotptr := self.ldSlot(index, unsafe.Sizeof(slot))
	entry := (*bpnode)(slotptr).ldEntry()
	(*lfslice)(entry).Insert(chunk)
	//--------
	// slot = self.slots[index]
	// slot.entry.Insert(chunk)
	if self.stats != nil {
		atomic.AddUint64(&self.stats.blocks[index].rels, 1)
	}
}

func (self *BuffPool) ldSlot(index int, size uintptr) unsafe.Pointer {
	var (
		nptr    unsafe.Pointer  = unsafe.Pointer(&self.slots)
		bin     *unsafe.Pointer = (*unsafe.Pointer)(unsafe.Pointer(uintptr(nptr) + size*uintptr(index)))
		slotptr unsafe.Pointer
	)
	slotptr = atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&bin)))
	return slotptr
}

func aptrTCAS(addr unsafe.Pointer, data unsafe.Pointer, target unsafe.Pointer, index uint32) bool {
	var bptr unsafe.Pointer
	tg := (*unsafe.Pointer)(unsafe.Pointer((uintptr)(addr) + unsafe.Sizeof(bptr)*uintptr(index)))
	ntu := unsafe.Pointer(tg)
	return atomic.CompareAndSwapPointer(
		(*unsafe.Pointer)(unsafe.Pointer(ntu)),
		(unsafe.Pointer)(unsafe.Pointer(target)),
		(unsafe.Pointer)(unsafe.Pointer(data)),
	)
}

func aptrCAS(data unsafe.Pointer, addr unsafe.Pointer, index uint32) bool {
	var bptr unsafe.Pointer
	target := (*unsafe.Pointer)(unsafe.Pointer(*(*uintptr)(addr) + unsafe.Sizeof(bptr)*uintptr(index)))
	ntu := unsafe.Pointer(target)
	return atomic.CompareAndSwapPointer(
		(*unsafe.Pointer)(unsafe.Pointer(ntu)),
		(unsafe.Pointer)(unsafe.Pointer(*target)),
		(unsafe.Pointer)(unsafe.Pointer(data)),
	)
}

func aptrStore(data unsafe.Pointer, addr unsafe.Pointer, index uint32) {
	var bptr unsafe.Pointer
	target := (*unsafe.Pointer)(unsafe.Pointer(*(*uintptr)(addr) + unsafe.Sizeof(bptr)*uintptr(index)))
	ntu := unsafe.Pointer(target)
	atomic.StorePointer(
		(*unsafe.Pointer)(unsafe.Pointer(ntu)),
		(unsafe.Pointer)(unsafe.Pointer(data)),
	)
}

func aptrLoad(addr unsafe.Pointer, index uint32) *[]byte {
	var bptr unsafe.Pointer
	target := (*unsafe.Pointer)(unsafe.Pointer(*(*uintptr)(addr) + unsafe.Sizeof(bptr)*uintptr(index)))
	val := atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(target)))
	return (*[]byte)(val)
}

func aptrSwap(data unsafe.Pointer, addr unsafe.Pointer, index uint32) unsafe.Pointer {
	var bptr unsafe.Pointer
	target := (*unsafe.Pointer)(unsafe.Pointer(*(*uintptr)(addr) + unsafe.Sizeof(bptr)*uintptr(index)))
	ntu := unsafe.Pointer(target)
	return atomic.SwapPointer(
		(*unsafe.Pointer)(unsafe.Pointer(ntu)),
		(unsafe.Pointer)(unsafe.Pointer(data)),
	)
}

func storePtr(data []byte, addr unsafe.Pointer, index uint32) {
	aptrStore(unsafe.Pointer(&data), addr, index)
}

func loadPtr(addr unsafe.Pointer, index uint32) *[]byte {
	return aptrLoad(addr, index)
}

func loadRPtr(addr unsafe.Pointer, index uint32) unsafe.Pointer {
	var bptr unsafe.Pointer
	target := (*unsafe.Pointer)(unsafe.Pointer((uintptr)(addr) + unsafe.Sizeof(bptr)*uintptr(index)))
	val := atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(target)))
	return val
}

func newlfslice() *lfslice {
	return &lfslice{}
}

func (self *lfslice) getPtr() unsafe.Pointer {
	if self == nil {
		return nil
	}
	return unsafe.Pointer(self)
}

func (self *markedPtr) Next() *lfslice {
	if self == nil {
		return nil
	}
	return (*lfslice)(self.next)
}

func (self *lfslice) getNextptr() unsafe.Pointer {
	if self == nil {
		return nil
	}
	return atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&self.next)))
}

func (self *lfslice) setNext(val unsafe.Pointer) bool {
	ptr := atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&self.next)))
	return atomic.CompareAndSwapPointer(
		(*unsafe.Pointer)(unsafe.Pointer(&self.next)),
		(unsafe.Pointer)(ptr),
		(unsafe.Pointer)(val),
	)
}

func (self *lfslice) insert(bd []byte) bool {
	addr := unsafe.Pointer(&self.data)
	for {
		var i uint32
		for i = cMin32; i < cLFSIZE; i++ {
			m := atomic.LoadUint32(&self.count)
			if m == cLFSIZE {
				return false
			}
			if aptrTCAS(addr, unsafe.Pointer(&bd), nil, i) {
				atomic.AddUint32(&self.count, 1)
				return true
			}
		}
	}
}

func (self *markedPtr) setMark(flag uint32) uint32 {
	return atomic.SwapUint32(&self.mark, flag)
}

func (self *markedPtr) getMark() uint32 {
	return atomic.LoadUint32(&self.mark)
}

func (self *lfslice) Len() uint32 {
	return atomic.LoadUint32(&self.count)
}

func (self *lfslice) Insert(bd []byte) bool {
	var nslc *markedPtr = nil
	for {
		if atomic.LoadUint32(&self.count) == cLFSIZE {
			n := atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&self.next)))
			if !atomic.CompareAndSwapPointer(
				(*unsafe.Pointer)(unsafe.Pointer(&self.next)),
				(unsafe.Pointer)(n),
				(unsafe.Pointer)(n),
			) {
				continue
			}
			if (*markedPtr)(n) == nil {
				nslc = &markedPtr{unsafe.Pointer(newlfslice()), 0}
			} else {
				nslc = (*markedPtr)(n)
			}

			if atomic.CompareAndSwapPointer(
				(*unsafe.Pointer)(unsafe.Pointer(&self.next)),
				(unsafe.Pointer)(n),
				(unsafe.Pointer)(nslc),
			) {
				return ((*lfslice)(nslc.next)).Insert(bd)
			}
		} else {
			if self.insert(bd) {
				return true
			}
		}
		runtime.Gosched()
	}
}

func (self *lfslice) get() []byte {
	addr := unsafe.Pointer(&self.data)
	var i uint32
	for {
		for i = cMin32; i < cLFSIZE; i++ {
			m := atomic.LoadUint32(&self.count)
			if m == 0 {
				return nil
			}
			cptr := loadRPtr(addr, i)
			if (*[]byte)(cptr) == nil {
				continue
			}
			// to ensure that concurrent/parallel calls
			// don't overlap.
			if !aptrTCAS(addr, cptr, cptr, i) {
				continue
			}
			if aptrTCAS(addr, nil, cptr, i) {
				val := (*[]byte)(cptr)
				if val != nil {
					// if atomic.AddUint32(&self.count, ^uint32(0)) < 0 {
					// 	atomic.StoreUint32(&self.count, 0)
					// }
					atomic.AddUint32(&self.count, ^uint32(0))
					return (*val)
				}
				return nil
			}
		}
		runtime.Gosched()
	}
}

func (self *lfslice) Get() []byte {
	for {
		if atomic.LoadUint32(&self.count) == 0 {
			n := atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&self.next)))
			if !atomic.CompareAndSwapPointer(
				(*unsafe.Pointer)(unsafe.Pointer(&self.next)),
				(unsafe.Pointer)(n),
				(unsafe.Pointer)(n),
			) {
				continue
			}
			nslc := (*markedPtr)(n)
			if nslc == nil {
				return nil
			}
			if atomic.CompareAndSwapPointer(
				(*unsafe.Pointer)(unsafe.Pointer(&self.next)),
				(unsafe.Pointer)(self.next),
				(unsafe.Pointer)(nslc),
			) {
				return ((*lfslice)(nslc.next)).Get()
			}
		} else {
			return self.get()
		}
		// NOTE:
	}
}

// TODO

func newlfqueue() *lfqueue {
	n := &qnode{}
	ptr := unsafe.Pointer(n)
	return &lfqueue{&qentry{ptr, ptr, 0}}
}

func (self *qentry) len() uint64 {
	return atomic.LoadUint64(&self.count)
}

func (self *qentry) inc() {
	atomic.AddUint64(&self.count, 1)
}
func (self *qentry) dec() {
	atomic.AddUint64(&self.count, ^uint64(0))
}

func (self *lfqueue) push(data *[]byte) {
	var (
		entry   unsafe.Pointer = unsafe.Pointer(&self.entry)
		node    unsafe.Pointer = unsafe.Pointer(&qnode{nil, data})
		tailptr unsafe.Pointer
		np      unsafe.Pointer
	)
	for {
		tailptr = atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(*(*uintptr)(entry) + unsafe.Sizeof(entry)*uintptr(1))))
		np = atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer((uintptr)(tailptr) + unsafe.Sizeof(entry)*uintptr(0))))
		if tailptr == atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(*(*uintptr)(entry)+unsafe.Sizeof(entry)*uintptr(1)))) {
			if (*qnode)(np) == nil {
				if atomic.CompareAndSwapPointer(
					(*unsafe.Pointer)(unsafe.Pointer(&((*qnode)(tailptr).next))),
					(unsafe.Pointer)(np),
					unsafe.Pointer(node),
				) {
					break
				}
			} else {
				atomic.CompareAndSwapPointer(
					(*unsafe.Pointer)(unsafe.Pointer(&self.entry.tail)),
					(unsafe.Pointer)(unsafe.Pointer(tailptr)),
					(unsafe.Pointer)(unsafe.Pointer(np)),
				)
			}
		}
		runtime.Gosched()
	}
	atomic.CompareAndSwapPointer(
		(*unsafe.Pointer)(unsafe.Pointer(&self.entry.tail)),
		(unsafe.Pointer)(tailptr),
		unsafe.Pointer(node),
	)
	self.entry.inc()
}

func (self *lfqueue) pop() []byte {
	var (
		entry   unsafe.Pointer = unsafe.Pointer(&self.entry)
		headptr unsafe.Pointer
		tailptr unsafe.Pointer
		np      unsafe.Pointer
		data    *[]byte
	)
	for {
		headptr = atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(*(*uintptr)(entry) + unsafe.Sizeof(entry)*uintptr(0))))
		tailptr = atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(*(*uintptr)(entry) + unsafe.Sizeof(entry)*uintptr(1))))
		np = atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer((uintptr)(headptr) + unsafe.Sizeof(entry)*uintptr(0))))
		if headptr == atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(*(*uintptr)(entry)+unsafe.Sizeof(entry)*uintptr(0)))) {
			if headptr == tailptr {
				if (*qnode)(np) == nil {
					return nil
				}
				atomic.CompareAndSwapPointer(
					(*unsafe.Pointer)(unsafe.Pointer(&self.entry.tail)),
					(unsafe.Pointer)(unsafe.Pointer(tailptr)),
					(unsafe.Pointer)(unsafe.Pointer(np)),
				)
			} else {
				data = (*qnode)(np).data
				if atomic.CompareAndSwapPointer(
					(*unsafe.Pointer)(unsafe.Pointer(&self.entry.head)),
					(unsafe.Pointer)(unsafe.Pointer(headptr)),
					(unsafe.Pointer)(unsafe.Pointer(np)),
				) {
					return *data
				}
			}
		}
		runtime.Gosched()
	}
}

func (self *lfqueue) detach() {
	var (
		ptr     unsafe.Pointer = unsafe.Pointer(&qnode{})
		nodeptr unsafe.Pointer = unsafe.Pointer(&qentry{ptr, ptr, 0})
		entry   unsafe.Pointer
	)
	// runtime.LockOSThread()
	// defer runtime.UnlockOSThread()
	for {

		entry = unsafe.Pointer(self.entry)
		if atomic.CompareAndSwapPointer(
			(*unsafe.Pointer)(unsafe.Pointer(&self.entry)),
			(unsafe.Pointer)(unsafe.Pointer(entry)),
			(unsafe.Pointer)(unsafe.Pointer(nodeptr)),
		) {
			break
		}
		runtime.Gosched()
	}
	// TODO
	// . do something with old head pointer
}

func (self *Buffer) Release() {
	// NOTE
	// . buffer should not be used after release
	if self.mp != nil {
		if self.auto {
			self.mp.AutoRelease(self.Data)
		} else {
			self.mp.Release(self.Data)
		}
		self.mp = nil
	}
}

func (self *Buffer) Bytes() []byte {
	return self.Data
}

func (self *Buffer) Reset() {
	self.Data = self.Data[:0]
}

func (self *Buffer) Len() int {
	return len(self.Data)
}

func (self *Buffer) SetString(data string) {
	self.Data = append(self.Data[:0], data...)
}

func (self *Buffer) Set(p []byte) {
	self.Data = append(self.Data[:0], p...)
}

func (self *Buffer) Write(p []byte) (int, error) {
	self.Data = append(self.Data, p...)
	return len(p), nil
}

func (self *Buffer) WriteString(s string) (int, error) {
	self.Data = append(self.Data, s...)
	return len(s), nil
}

func (self *Buffer) WriteTo(writer io.Writer) (int64, error) {
	n, err := writer.Write(self.Data)
	return int64(n), err
}

func (self *Buffer) WriteByte(c byte) error {
	self.Data = append(self.Data, c)
	return nil
}

func (self *Buffer) ReadFrom(reader io.Reader) (int64, error) {
	var (
		buff []byte = self.Data
		s, e int64  = int64(len(buff)), int64(cap(buff))
		n    int64  = s
	)
	if e == 0 {
		e = 64
		buff = make([]byte, e)
	} else {
		buff = buff[:e]
	}
	for {
		if n == e {
			e *= 2
			nb := make([]byte, e)
			copy(nb, buff)
			buff = nb
		}
		nr, err := reader.Read(buff[n:])
		n += int64(nr)
		if err != nil {
			self.Data = buff[:n]
			n -= s
			if err == io.EOF {
				return n, nil
			}
			return n, err
		}
	}
}

func (self *Buffer) String() string {
	return string(self.Data)
}

func (self *Stats) adapt() {
	if !atomic.CompareAndSwapUint32(&self.safe, 0, 1) {
		return
	}
	var (
		max, smax uint64
		min       int64    = -1
		sum       uint64   = 0
		steps     int      = len(self.blocks)
		blklen    uint64   = uint64(steps)
		n         []lbstat = make([]lbstat, 0, steps)
	)
	for i := uint64(0); i < blklen; i++ {
		allocs := atomic.SwapUint64(&self.blocks[i].rels, 0)
		var size uint64 = 64 << i
		if min == -1 || int64(size) < min {
			min = int64(size)
		}
		sum += allocs
		n = append(n, lbstat{
			allocs: allocs,
			size:   size,
		})
	}
	max, smax, sum = uint64(min), uint64(float64(sum)*percentile), 0
	for i := 0; i < steps; i++ {
		if sum < smax {
			sum += n[i].allocs
			size := n[i].size
			if size > max {
				max = size
			}
			continue
		}
		break
	}
	atomic.StoreUint64(&self.defbs, uint64(min))
	atomic.StoreUint64(&self.max, maxSize)
	atomic.StoreUint32(&self.safe, 0)
}
