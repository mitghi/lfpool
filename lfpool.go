/* MIT License
*
* Copyright (c) 2018 Mike Taghavi <mitghi[at]gmail.com>
*
* Permission is hereby granted, free of charge, to any person obtaining a copy
* of this software and associated documentation files (the "Software"), to deal
* in the Software without restriction, including without limitation the rights
* to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
* copies of the Software, and to permit persons to whom the Software is
* furnished to do so, subject to the following conditions:
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
 */

package lfpool

import (
	"errors"
	"io"
	"runtime"
	"sync/atomic"
	"unsafe"
)

/**
* TODO:
* . finish documentation.
**/

// - MARK: base.

const (
	lBlkMin   = 0xffffffc0
	lBlkMax   = 0x02000000
	cMin32    = 0x00000000
	cDeBruijn = 0x07C4ACDD
	cLFSize   = 16
)

const (
	calthld    = 42000
	percentile = 0.95
	minSize    = 64
	maxSize    = 2097152
)

var (
	_ptr_   unsafe.Pointer
	ptrSize uintptr = unsafe.Sizeof(_ptr_)
)

var (
	LPNotSupported error = errors.New("lfpool: op not supported.")
)

var mDeBruijnBitPosition [32]int = [32]int{
	0, 9, 1, 10, 13, 21, 2, 29, 11, 14, 16, 18, 22, 25, 3, 30,
	8, 12, 20, 28, 15, 17, 24, 7, 19, 27, 23, 6, 26, 5, 4, 31,
}

var blocks blktable = blktable{
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

type blktable []int

type LFPool struct {
	slots [32]pslot
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

type lbstat struct {
	allocs uint64
	size   uint64
}

type markedPtr struct {
	next unsafe.Pointer // *lfslice
	mark uint32
}

type lfslice struct {
	data  [cLFSize]unsafe.Pointer
	count uint32
	next  unsafe.Pointer // *markedPtr
}

type pslot struct {
	entry unsafe.Pointer // *lfslice
	flag  uint32
}

type Buffer struct {
	Data []byte
	mp   *LFPool
	auto bool
}

// - MARK: alloc/init section.

// NewLFPool initializes and allocates a new
// `LFPool` and returns a pointer to it.
func NewLFPool() *LFPool {
	var lfp *LFPool = &LFPool{stats: nil}
	for i, _ := range lfp.slots {
		lfp.slots[i].entry = unsafe.Pointer(newlfslice())
	}
	return lfp
}

// NewLFPoolWithStats initializes and
// allocates a new `LFPool` with an
// embedded statistics struct `Stats`
// and returns a pointer to it.
func NewLFPoolWithStats() *LFPool {
	var lfp *LFPool = &LFPool{stats: &Stats{}}
	for i, _ := range lfp.slots {
		lfp.slots[i].entry = unsafe.Pointer(newlfslice())
	}
	return lfp
}

// newlfslice initializes and allocates
// a new `lfslice` and returns a pointer
// to it.
func newlfslice() *lfslice {
	return &lfslice{}
}

// - MARK: blktable section.

// bin returns the nearest log of nearest
// power of two from its table. It is used
// to find bin slots.
func (b blktable) bin(num int) int {
	return int(b.lgb2(uint32(num)))
}

// size returns nearest power of two from
// its table.
func (b blktable) size(num int) int {
	return b[int(b.bin(num))]
}

// nextPow2 returns next power of two.
func (b blktable) nextPow2(num uint32) uint32 {
	num--
	num |= num >> 1
	num |= num >> 2
	num |= num >> 4
	num |= num >> 8
	num |= num >> 16
	num++

	return num
}

// lgb2 returns log of two.
func (b blktable) lgb2(num uint32) int {
	var npw uint32 = uint32((b.nextPow2(num)-1)*(uint32)(0x07C4ACDD)) >> 27
	return mDeBruijnBitPosition[int(npw)]
}

// - MARK: pslot section.

func (ps *pslot) ldEntry() unsafe.Pointer {
	return atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&ps.entry)))
}

func (ps *pslot) detach() *lfslice {
	var (
		hptr unsafe.Pointer
	)
	for {
		hptr = atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&ps.entry)))
		if atomic.CompareAndSwapPointer(
			(*unsafe.Pointer)(unsafe.Pointer(&ps.entry)),
			(unsafe.Pointer)(unsafe.Pointer(ps.entry)),
			(unsafe.Pointer)(unsafe.Pointer(newlfslice())),
		) {
			break
		}
	}
	return (*lfslice)(hptr)
}

// - MARK: LFPool section.

func (lfp *LFPool) cleanUp(head *lfslice) {
	// TODO:
	//  . do cleanup, sorting, ....
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// for curr := head.Next(); (*lfslice)(curr) != nil; an = ((*markedPtr)((*lfslice)(curr).nextPtr()).Next()) {
	// }
}

func (lfp *LFPool) Get(chunks ...int) []byte {
	cl := len(chunks)
	switch cl {
	case 1:
		chunk := lfp.getChunk(chunks[0])
		chunk = chunk[:cap(chunk)]
		return chunk
	case 2:
		if chunks[0] > chunks[1] {
			panic("core(pool): len>cap")
		}
		chunk := lfp.getChunk(chunks[1])
		chunk = chunk[:chunks[0]]
		return chunk
	default:
		chunk := lfp.getChunk(0) // 64
		chunk = chunk[:cap(chunk)]
		return chunk
	}
}

func (lfp *LFPool) AutoGet() ([]byte, error) {
	if lfp.stats != nil && atomic.LoadUint32(&lfp.stats.auto) != 0 {
		chunk := lfp.Get(int(atomic.LoadUint64(&lfp.stats.defbs)))
		return chunk, nil
	}
	return nil, LPNotSupported
}

func (lfp *LFPool) GetAutoBuffer() (*Buffer, error) {
	chunk, err := lfp.AutoGet()
	if err != nil {
		return nil, err
	}
	b := &Buffer{chunk, lfp, true}
	b.Reset()
	return b, nil
}

func (lfp *LFPool) GetBuffer(chunks ...int) *Buffer {
	b := &Buffer{lfp.Get(chunks...), lfp, false}
	b.Reset()
	return b
}

func (lfp *LFPool) Release(chunk []byte) {
	lfp.releaseChunk(chunk)
}

func (lfp *LFPool) ReleaseBuffer(b *Buffer) {
	if b != nil {
		lfp.releaseChunk(b.Data)
	}
}

func (lfp *LFPool) AutoReleaseBuffer(b *Buffer) {
	if b != nil && b.auto {
		lfp.AutoRelease(b.Data)
	}
}

func (lfp *LFPool) AutoRelease(chunk []byte) {
	var (
		capacity = cap(chunk)
		slot     pslot
		nc       uint64
		np       int
		index    int
	)
	if ((capacity & lBlkMin) == 0) || (capacity > lBlkMax) {
		return
	} else {
		index = blocks.lgb2(uint32(capacity))
	}
	nc = atomic.LoadUint64(&lfp.stats.max)
	if capacity <= int(nc) {
		np = blocks[index]
		if capacity < int(np) {
			ctmp := make([]byte, np)
			copy(ctmp, chunk)
			chunk = ctmp
		}
		slotptr := lfp.ldSlot(index, unsafe.Sizeof(slot))
		entry := (*pslot)(slotptr).ldEntry()
		(*lfslice)(entry).Insert(chunk)
	}
}

func (lfp *LFPool) getChunk(chunk int) []byte {
	var (
		slot     pslot
		ret      []byte
		capacity int
		index    int
		entry    unsafe.Pointer
		slotPtr  unsafe.Pointer
	)
	if (chunk & lBlkMin) == 0 {
		index = blocks.lgb2(0x3f)
	} else if uint32(chunk) >= lBlkMax {
		index = blocks.lgb2(0x01ffffff)
	} else {
		index = blocks.lgb2(uint32(chunk - 1))
	}
	capacity = blocks[index]
	slotPtr = lfp.ldSlot(index, unsafe.Sizeof(slot))
	entry = (*pslot)(slotPtr).ldEntry()
	ret = (*lfslice)(entry).Get()
	if ret == nil {
		if lfp.stats != nil {
			atomic.AddUint64(&lfp.stats.blocks[index].allocs, 1)
		}
		return make([]byte, capacity)
	}
	return ret
}

func (lfp *LFPool) releaseChunk(chunk []byte) {
	var (
		capacity = cap(chunk)
		slot     pslot
		np       int
		index    int
		entry    unsafe.Pointer
		slotPtr  unsafe.Pointer
	)
	if ((capacity & lBlkMin) == 0) || (capacity > lBlkMax) {
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
	slotPtr = lfp.ldSlot(index, unsafe.Sizeof(slot))
	entry = (*pslot)(slotPtr).ldEntry()
	(*lfslice)(entry).Insert(chunk)
	if lfp.stats != nil {
		atomic.AddUint64(&lfp.stats.blocks[index].rels, 1)
	}
}

func (lfp *LFPool) ldSlot(index int, size uintptr) unsafe.Pointer {
	var (
		bin     *unsafe.Pointer
		slotPtr unsafe.Pointer
		nptr    unsafe.Pointer = unsafe.Pointer(&lfp.slots)
	)
	bin = (*unsafe.Pointer)(unsafe.Pointer(uintptr(nptr) + (size * uintptr(index))))
	slotPtr = atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&bin)))
	return slotPtr
}

// - MARK: lfslice section.

func ptrCAS(addr unsafe.Pointer, data unsafe.Pointer, target unsafe.Pointer, index uint32) bool {
	var (
		tptr *unsafe.Pointer
		cptr unsafe.Pointer
	)
	tptr = (*unsafe.Pointer)(unsafe.Pointer((uintptr)(addr) + (ptrSize * uintptr(index))))
	cptr = unsafe.Pointer(tptr)
	return atomic.CompareAndSwapPointer((*unsafe.Pointer)(unsafe.Pointer(cptr)),
		(unsafe.Pointer)(unsafe.Pointer(target)),
		(unsafe.Pointer)(unsafe.Pointer(data)),
	)
}

func loadPtr(addr unsafe.Pointer, index uint32) unsafe.Pointer {
	var (
		tptr *unsafe.Pointer
	)
	tptr = (*unsafe.Pointer)(unsafe.Pointer((uintptr)(addr) + (ptrSize * uintptr(index))))
	return atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(tptr)))
}

func (lfs *lfslice) getPtr() unsafe.Pointer {
	if lfs == nil {
		return nil
	}
	return unsafe.Pointer(lfs)
}

func (lfs *markedPtr) Next() *lfslice {
	if lfs == nil {
		return nil
	}
	return (*lfslice)(lfs.next)
}

func (lfs *lfslice) nextPtr() unsafe.Pointer {
	if lfs == nil {
		return nil
	}
	return atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&lfs.next)))
}

func (lfs *lfslice) setNext(val unsafe.Pointer) bool {
	var (
		ptr unsafe.Pointer = atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&lfs.next)))
	)
	return atomic.CompareAndSwapPointer(
		(*unsafe.Pointer)(unsafe.Pointer(&lfs.next)),
		(unsafe.Pointer)(ptr),
		(unsafe.Pointer)(val),
	)
}

func (lfs *lfslice) insert(bd []byte) bool {
	var (
		i    uint32
		addr unsafe.Pointer = unsafe.Pointer(&lfs.data)
	)
	for {
		for i = cMin32; i < cLFSize; i++ {
			if atomic.LoadUint32(&lfs.count) == cLFSize {
				return false
			}
			if ptrCAS(addr, unsafe.Pointer(&bd), nil, i) {
				atomic.AddUint32(&lfs.count, 1)
				return true
			}
		}
	}
}

func (lfs *lfslice) Len() uint32 {
	return atomic.LoadUint32(&lfs.count)
}

func (lfs *lfslice) Insert(bd []byte) bool {
	var (
		n    unsafe.Pointer
		nslc *markedPtr
	)
	for {
		if atomic.LoadUint32(&lfs.count) == cLFSize {
			n = atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&lfs.next)))
			if !atomic.CompareAndSwapPointer(
				(*unsafe.Pointer)(unsafe.Pointer(&lfs.next)),
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
				(*unsafe.Pointer)(unsafe.Pointer(&lfs.next)),
				(unsafe.Pointer)(n),
				(unsafe.Pointer)(nslc),
			) {
				return ((*lfslice)(nslc.next)).Insert(bd)
			}
		} else {
			if lfs.insert(bd) {
				return true
			}
		}
		runtime.Gosched()
	}
}

func (lfs *lfslice) get() []byte {
	var (
		i      uint32
		v      *[]byte
		target *unsafe.Pointer
		vptr   unsafe.Pointer
		addr   unsafe.Pointer = unsafe.Pointer(&lfs.data)
	)
	for {
		for i = cMin32; i < cLFSize; i++ {
			if atomic.LoadUint32(&lfs.count) == 0 {
				return nil
			}
			target = (*unsafe.Pointer)(unsafe.Pointer((uintptr)(addr) + (ptrSize * uintptr(i))))
			vptr = atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(target)))
			if (*[]byte)(vptr) == nil {
				continue
			}
			// prevent overlapping parallel calls
			if !ptrCAS(addr, vptr, vptr, i) {
				continue
			}
			if ptrCAS(addr, nil, vptr, i) {
				v = (*[]byte)(vptr)
				if v != nil {
					atomic.AddUint32(&lfs.count, ^uint32(0))
					return (*v)
				}
				return nil
			}
		}
		runtime.Gosched()
	}
}

func (lfs *lfslice) Get() []byte {
	var (
		n    unsafe.Pointer
		nslc *markedPtr
	)
	for {
		if atomic.LoadUint32(&lfs.count) == 0 {
			n = atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&lfs.next)))
			if !atomic.CompareAndSwapPointer(
				(*unsafe.Pointer)(unsafe.Pointer(&lfs.next)),
				(unsafe.Pointer)(n),
				(unsafe.Pointer)(n),
			) {
				continue
			}
			nslc = (*markedPtr)(n)
			if nslc == nil {
				return nil
			}
			if atomic.CompareAndSwapPointer(
				(*unsafe.Pointer)(unsafe.Pointer(&lfs.next)),
				(unsafe.Pointer)(lfs.next),
				(unsafe.Pointer)(nslc),
			) {
				return ((*lfslice)(nslc.next)).Get()
			}
		} else {
			return lfs.get()
		}
	}
}

// - MARK: markedPtr section.

func (mp *markedPtr) setMark(flag uint32) uint32 {
	return atomic.SwapUint32(&mp.mark, flag)
}

func (mp *markedPtr) getMark() uint32 {
	return atomic.LoadUint32(&mp.mark)
}

// - MARK: Buffer section.

func (b *Buffer) Release() {
	// NOTE
	// . buffer should not be used after release
	if b.mp != nil {
		if b.auto {
			b.mp.AutoRelease(b.Data)
		} else {
			b.mp.Release(b.Data)
		}
		b.mp = nil
	}
}

func (b *Buffer) Bytes() []byte {
	return b.Data
}

func (b *Buffer) Reset() {
	b.Data = b.Data[:0]
}

func (b *Buffer) Len() int {
	return len(b.Data)
}

func (b *Buffer) SetString(data string) {
	b.Data = append(b.Data[:0], data...)
}

func (b *Buffer) Set(p []byte) {
	b.Data = append(b.Data[:0], p...)
}

func (b *Buffer) Write(p []byte) (int, error) {
	b.Data = append(b.Data, p...)
	return len(p), nil
}

func (b *Buffer) WriteString(s string) (int, error) {
	b.Data = append(b.Data, s...)
	return len(s), nil
}

func (b *Buffer) WriteTo(writer io.Writer) (int64, error) {
	n, err := writer.Write(b.Data)
	return int64(n), err
}

func (b *Buffer) WriteByte(c byte) error {
	b.Data = append(b.Data, c)
	return nil
}

func (b *Buffer) ReadFrom(reader io.Reader) (int64, error) {
	var (
		buff []byte = b.Data
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
			b.Data = buff[:n]
			n -= s
			if err == io.EOF {
				return n, nil
			}
			return n, err
		}
	}
}

func (b *Buffer) String() string {
	return string(b.Data)
}

// - MARK: Stats section.

func (s *Stats) adapt() {
	if !atomic.CompareAndSwapUint32(&s.safe, 0, 1) {
		return
	}
	var (
		max, smax uint64
		min       int64    = -1
		sum       uint64   = 0
		steps     int      = len(s.blocks)
		blklen    uint64   = uint64(steps)
		n         []lbstat = make([]lbstat, 0, steps)
	)
	for i := uint64(0); i < blklen; i++ {
		allocs := atomic.SwapUint64(&s.blocks[i].rels, 0)
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
	atomic.StoreUint64(&s.defbs, uint64(min))
	atomic.StoreUint64(&s.max, maxSize)
	atomic.StoreUint32(&s.safe, 0)
}
