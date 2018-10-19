package lfpool

import (
	"io"
	"sync/atomic"
)

type Buffer struct {
	Data []byte
	mp   *BuffPool
	auto bool
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
