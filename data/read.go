package data

import (
	"code.google.com/p/mx3/util"
	"fmt"
	"hash"
	"hash/crc64"
	"io"
	"math"
	"os"
	"unsafe"
)

func Read(in io.Reader) (data *Slice, time float64, err error) {
	r := newReader(in)
	return r.readSlice()
}

func ReadFile(fname string) (data *Slice, time float64, err error) {
	f, err := os.Open(fname)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	return Read(f)
}

func MustReadFile(fname string) (data *Slice, time float64) {
	s, t, err := ReadFile(fname)
	util.FatalErr(err)
	return s, t
}

// Reads successive data frames in dump format.
type reader struct {
	in  io.Reader
	crc hash.Hash64
	err error
}

func newReader(in io.Reader) *reader {
	r := new(reader)
	r.in = in
	r.crc = crc64.New(table)
	return r
}

func (r *reader) readSlice() (slice *Slice, time float64, err error) {
	r.err = nil // clear previous error, if any
	magic := r.readString()
	if r.err != nil {
		return nil, 0, r.err
	}
	if magic != MAGIC {
		r.err = fmt.Errorf("dump: bad magic number:%v", magic)
		return nil, 0, r.err
	}
	nComp := r.readInt()
	size := [3]int{}
	for i := range size {
		size[i] = r.readInt()
	}
	cell := [3]float64{}
	for i := range cell {
		cell[i] = r.readFloat64()
	}
	_ = r.readString() // mesh unit
	time = r.readFloat64()
	_ = r.readString() // time unit

	mesh := NewMesh(size[0], size[1], size[2], cell[0], cell[1], cell[2])
	s := NewSlice(nComp, mesh)

	s.tag = r.readString()
	s.unit = r.readString()
	precission := r.readUint64()
	util.Assert(precission == 4)

	for i := 0; i < padding; i++ {
		_ = r.readUint64()
	}

	if r.err != nil {
		return
	}

	host := s.Host()
	length := mesh.NCell()
	for _, data := range host {
		buf := (*(*[1<<31 - 1]byte)(unsafe.Pointer(&data[0])))[0 : SIZEOF_FLOAT32*length]
		r.read(buf)
	}

	// Check CRC
	var mycrc uint64 // checksum by this reader
	if r.crc != nil {
		mycrc = r.crc.Sum64()
	}
	storedcrc := r.readUint64() // checksum from data stream. 0 means not set
	if r.err != nil {
		return nil, 0, r.err
	}
	if r.crc != nil {
		r.crc.Reset() // reset for next frame
	}
	if r.crc != nil && storedcrc != 0 && mycrc != storedcrc {
		r.err = fmt.Errorf("dump CRC error: expected %16x, got %16x", storedcrc, mycrc)
		return nil, 0, r.err
	}

	return s, time, nil
}

func (r *reader) readInt() int {
	x := r.readUint64()
	if uint64(int(x)) != x {
		r.err = fmt.Errorf("value overflows int: %v", x)
	}
	return int(x)
}

// read until the buffer is full
func (r *reader) read(buf []byte) {
	_, err := io.ReadFull(r.in, buf[:])
	if err != nil {
		r.err = err
	}
	if r.crc != nil {
		r.crc.Write(buf)
	}
}

// read a maximum 8-byte string
func (r *reader) readString() string {
	var buf [8]byte
	r.read(buf[:])
	// trim trailing NULs.
	i := 0
	for i = 0; i < len(buf); i++ {
		if buf[i] == 0 {
			break
		}
	}
	return string(buf[:i])
}

func (r *reader) readFloat64() float64 {
	return math.Float64frombits(r.readUint64())
}

func (r *reader) readUint64() uint64 {
	var buf [8]byte
	r.read(buf[:])
	return *((*uint64)(unsafe.Pointer(&buf[0])))
}
