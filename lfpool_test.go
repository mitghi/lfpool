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
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func TestExpA(t *testing.T) {
	fmt.Println(blocks[blocks.lgb2(63)])
	fmt.Println(blocks[blocks.lgb2(64)])
	fmt.Println(blocks.lgb2(4096))
	fmt.Println(blocks[blocks.lgb2(4096)])
	sl := newlfslice()
	for i := 0; i < 64; i++ {
		sl.Insert([]byte("test"))
	}
	ptr := atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&sl.next)))
	fmt.Printf("ptr(%p)\n", ptr)
	n := (*markedPtr)(ptr)
	fmt.Println(n.Next(), sl)
	// for an := n.Next(); (*lfslice)(an) != nil; an = ((*markedPtr)((*lfslice)(an).getNextptr()).Next()) {
	// 	fmt.Println(an)
	// }
	for i := 0; i < 30; i++ {
		sl.Get()
	}
	fmt.Println("----------------")
	fmt.Println(n.Next(), sl)
}

func TestExpB(t *testing.T) {
	var (
		bp *BuffPool       = NewBuffPool()
		wg *sync.WaitGroup = &sync.WaitGroup{}
	)
	ch := bp.Get(67)
	bp.Release(ch)
	bp.Get(66)
	fmt.Println(ch, len(ch), cap(ch))
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(bp *BuffPool, wg *sync.WaitGroup) {
			for i := 0; i < 100; i++ {
				ch := bp.Get(rand.Intn(65535))
				time.Sleep(time.Millisecond * time.Duration(rand.Intn(50)))
				bp.Release(ch)
			}
			wg.Done()
		}(bp, wg)

		wg.Add(1)
		go func(bp *BuffPool, wg *sync.WaitGroup) {
			for i := 0; i < 100; i++ {
				ch := bp.Get(0)
				if rand.Int()%2 == 0 {
					bp.Release(ch)
				}
			}
			wg.Done()
		}(bp, wg)
	}
	wg.Wait()

	for i := 0; i < 22; i++ {
		fmt.Printf("entry: %+v\n", bp.slots[i].entry)
		if bp.stats != nil {
			fmt.Printf("stats: %+v\n", bp.stats.blocks[i])
		}
	}
	// for an := bp.Next(); (*lfslice)(an) != nil; an = ((*markedPtr)((*lfslice)(an).getNextptr()).Next()) {
	// }
}

func TestExpD(t *testing.T) {
	bp := NewBuffPool()
	b := bp.GetBuffer(63)
	if cap(b.Data) != 64 {
		t.Fatal("invalid capacity")
	}
	b.WriteString("testing")
	if res := b.String(); res != "testing" && b.Len() != 7 {
		t.Fatal("invalid results", b.Len(), res)
	}
}

func TestExpE(t *testing.T) {
	var (
		bp *BuffPool       = NewBuffPool()
		wg *sync.WaitGroup = &sync.WaitGroup{}
	)
	ch := bp.Get(67)
	bp.Release(ch)
	bp.Get(66)
	fmt.Println(ch, len(ch), cap(ch))
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(bp *BuffPool, wg *sync.WaitGroup) {
			for i := 0; i < 100; i++ {
				ch := bp.Get(rand.Intn(65535))
				time.Sleep(time.Millisecond * time.Duration(rand.Intn(50)))
				bp.Release(ch)
			}
			wg.Done()
		}(bp, wg)

		wg.Add(1)
		go func(bp *BuffPool, wg *sync.WaitGroup) {
			for i := 0; i < 100; i++ {
				ch := bp.Get(0)
				if rand.Int()%2 == 0 {
					bp.Release(ch)
				}
			}
			wg.Done()
		}(bp, wg)
	}

	wg.Wait()

	for i := 0; i < 22; i++ {
		fmt.Printf("entry: %+v\n", bp.slots[i].entry)
		if bp.stats != nil {
			fmt.Printf("stats: %+v\n", bp.stats.blocks[i])
		}
		fmt.Printf("------block:   %d   ----------------\n", blocks[i])
	}
}

func TestExpF(t *testing.T) {
	var (
		bp *BuffPool       = WithStats()
		wg *sync.WaitGroup = &sync.WaitGroup{}
	)
	ch := bp.Get(67)
	bp.Release(ch)
	bp.Get(66)
	fmt.Println(ch, len(ch), cap(ch))
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(bp *BuffPool, wg *sync.WaitGroup) {
			for i := 0; i < 100; i++ {
				ch := bp.Get(rand.Intn(65535))
				time.Sleep(time.Millisecond * time.Duration(rand.Intn(50)))
				bp.Release(ch)
			}
			wg.Done()
		}(bp, wg)

		wg.Add(1)
		go func(bp *BuffPool, wg *sync.WaitGroup) {
			for i := 0; i < 100; i++ {
				ch := bp.Get(0)
				if rand.Int()%2 == 0 {
					bp.Release(ch)
				}
			}
			wg.Done()
		}(bp, wg)
		wg.Add(1)
		go func(bp *BuffPool, wg *sync.WaitGroup) {
			for i := 0; i < 5; i++ {
				time.Sleep(time.Millisecond * 50)
				_ = bp.slots[rand.Intn(21)]
			}
			wg.Done()
		}(bp, wg)
	}

	wg.Wait()

	for i := 0; i < 22; i++ {
		fmt.Printf("entry: %+v\n", bp.slots[i].entry)
		if bp.stats != nil {
			fmt.Printf("stats: %+v\n", bp.stats.blocks[i])
		}
		fmt.Printf("------block:   %d   ----------------\n", blocks[i])
	}
}

func TestExpG(t *testing.T) {
	var (
		bp *BuffPool       = NewBuffPool()
		wg *sync.WaitGroup = &sync.WaitGroup{}
	)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(bp *BuffPool, wg *sync.WaitGroup) {
			for i := 0; i < 100; i++ {
				ch := bp.Get(rand.Intn(65535))
				time.Sleep(time.Millisecond * time.Duration(rand.Intn(50)))
				bp.Release(ch)
			}
			wg.Done()
		}(bp, wg)

		wg.Add(1)
		go func(bp *BuffPool, wg *sync.WaitGroup) {
			for i := 0; i < 100; i++ {
				ch := bp.Get(0)
				if rand.Int()%2 == 0 {
					bp.Release(ch)
				}
			}
			wg.Done()
		}(bp, wg)
		wg.Add(1)
		go func(bp *BuffPool, wg *sync.WaitGroup) {
			var _s bpnode
			var ssize uintptr = unsafe.Sizeof(_s)
			for i := 0; i < 5; i++ {
				time.Sleep(time.Millisecond * time.Duration(rand.Intn(50)))
				slot := bp.ldSlot(rand.Intn(32), ssize)
				_ = (*bpnode)(slot).detach()
			}
			wg.Done()
		}(bp, wg)
	}

	wg.Wait()

	for i := 0; i < 22; i++ {
		fmt.Printf("entry: %+v\n", bp.slots[i].entry)
		if bp.stats != nil {
			fmt.Printf("stats: %+v\n", bp.stats.blocks[i])
		}
		fmt.Printf("------block:   %d   ----------------\n", blocks[i])
	}
}

func TestExpH(t *testing.T) {
	bp := NewBuffPool()
	a := bp.Get()
	fmt.Println(len(a), cap(a))
	if ac, al := cap(a), len(a); ac != 64 && al != 64 {
		t.Fatal("invalid", ac, al)
	}
	b := bp.Get(8)
	fmt.Println(len(b), cap(b))
	if bc, bl := cap(b), len(b); bc != 64 && bl != 64 {
		t.Fatal("invalid", bc, bl)
	}
	c := bp.Get(8, 8)
	fmt.Println(len(c), cap(c))
	if cc, cl := cap(c), len(c); cc != 64 && cl != 8 {
		t.Fatal("invalid", cc, cl)
	}
}
