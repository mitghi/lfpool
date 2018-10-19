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
	"runtime"
	"sync/atomic"
	"unsafe"
)

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

func loadRPtr(addr unsafe.Pointer, index uint32) unsafe.Pointer {
	var bptr unsafe.Pointer
	target := (*unsafe.Pointer)(unsafe.Pointer((uintptr)(addr) + unsafe.Sizeof(bptr)*uintptr(index)))
	val := atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(target)))
	return val
}
