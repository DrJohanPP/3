package data

// Slice stores N-component GPU or host data.

import (
	"bytes"
	"github.com/mumax/3/util"
	"log"
	"reflect"
	"unsafe"
)

// Slice is like a [][]float32, but may be stored in GPU or host memory.
type Slice struct {
	ptr_    [MAX_COMP]unsafe.Pointer // keeps data local // TODO: rm (premature optimization)
	ptrs    []unsafe.Pointer         // points into ptr_
	size    [3]int
	len_    int
	memType int8
}

// this package must not depend on CUDA. If CUDA is
// loaded, these functions are set to cu.MemFree, ...
// NOTE: cpyDtoH and cpuHtoD are only needed to support 32-bit builds,
// otherwise, it could be removed in favor of memCpy only.
var (
	memFree, memFreeHost           func(unsafe.Pointer)
	memCpy, memCpyDtoH, memCpyHtoD func(dst, src unsafe.Pointer, bytes int64)
)

// Internal: enables slices on GPU. Called upon cuda init.
func EnableGPU(free, freeHost func(unsafe.Pointer),
	cpy, cpyDtoH, cpyHtoD func(dst, src unsafe.Pointer, bytes int64)) {
	memFree = free
	memFreeHost = freeHost
	memCpy = cpy
	memCpyDtoH = cpyDtoH
	memCpyHtoD = cpyHtoD
}

// Make a CPU Slice with nComp components of size length.
func NewSlice(nComp int, size [3]int) *Slice {
	length := prod(size)
	ptrs := make([]unsafe.Pointer, nComp)
	for i := range ptrs {
		ptrs[i] = unsafe.Pointer(&(make([]float32, length)[0]))
	}
	return SliceFromPtrs(size, CPUMemory, ptrs)
}

// Return a slice without underlying storage. Used to represent a mask containing all 1's.
func NilSlice(nComp int, size [3]int) *Slice {
	return SliceFromPtrs(size, UnifiedMemory, make([]unsafe.Pointer, nComp))
}

// Internal: construct a Slice using bare memory pointers. Used by cuda.
func SliceFromPtrs(size [3]int, memType int8, ptrs []unsafe.Pointer) *Slice {
	length := prod(size)
	nComp := len(ptrs)
	util.Argument(nComp > 0 && length > 0 && nComp <= MAX_COMP)
	s := new(Slice)
	s.ptrs = s.ptr_[:nComp]
	s.len_ = length
	s.size = size
	for c := range ptrs {
		s.ptrs[c] = ptrs[c]
	}
	s.memType = memType
	return s
}

func SliceFromList(data [][]float32, size [3]int) *Slice {
	ptrs := make([]unsafe.Pointer, len(data))
	for i := range ptrs {
		util.Argument(len(data[i]) == prod(size))
		ptrs[i] = unsafe.Pointer(&data[i][0])
	}
	return SliceFromPtrs(size, CPUMemory, ptrs)
}

const MAX_COMP = 3 // Maximum supported number of Slice components

// Frees the underlying storage and zeros the Slice header to avoid accidental use.
// Slices sharing storage will be invalid after Free. Double free is OK.
func (s *Slice) Free() {
	if s == nil {
		return
	}
	// free storage
	switch s.memType {
	case 0:
		return // already freed
	case GPUMemory:
		for _, ptr := range s.ptrs {
			memFree(ptr)
		}
	case UnifiedMemory:
		for _, ptr := range s.ptrs {
			memFreeHost(ptr)
		}
	case CPUMemory:
		// nothing to do
	default:
		panic("invalid memory type")
	}
	s.Disable()
}

// INTERNAL. Overwrite struct fields with zeros to avoid
// accidental use after Free.
func (s *Slice) Disable() {
	s.ptr_ = [MAX_COMP]unsafe.Pointer{}
	s.ptrs = s.ptrs[:0]
	s.len_ = 0
	s.size = [3]int{0, 0, 0}
	s.memType = 0
}

// value for Slice.memType
const (
	CPUMemory     = 1 << 0
	GPUMemory     = 1 << 1
	UnifiedMemory = CPUMemory | GPUMemory
)

// MemType returns the memory type of the underlying storage:
// CPUMemory, GPUMemory or UnifiedMemory
func (s *Slice) MemType() int {
	return int(s.memType)
}

// GPUAccess returns whether the Slice is accessible by the GPU.
// true means it is either stored on GPU or in unified host memory.
func (s *Slice) GPUAccess() bool {
	return s.memType&GPUMemory != 0
}

// CPUAccess returns whether the Slice is accessible by the CPU.
// true means it is stored in host memory.
func (s *Slice) CPUAccess() bool {
	return s.memType&CPUMemory != 0
}

// NComp returns the number of components.
func (s *Slice) NComp() int {
	return len(s.ptrs)
}

// Len returns the number of elements per component.
func (s *Slice) Len() int {
	return int(s.len_)
}

// Mesh on which the data is defined.
//func (s *Slice) Mesh() *Mesh {
//	if s == nil {
//		return nil
//	} else {
//		return s.mesh
//	}
//}

func (s *Slice) Size() [3]int {
	if s == nil {
		return [3]int{0, 0, 0}
	}
	return s.size
}

// Comp returns a single component of the Slice.
func (s *Slice) Comp(i int) *Slice {
	sl := new(Slice)
	sl.ptr_[0] = s.ptrs[i]
	sl.ptrs = sl.ptr_[:1]
	sl.size = s.size
	sl.len_ = s.len_
	sl.memType = s.memType
	return sl
}

// DevPtr returns a CUDA device pointer to a component.
// Slice must have GPUAccess.
// It is safe to call on a nil slice, returns NULL.
func (s *Slice) DevPtr(component int) unsafe.Pointer {
	if s == nil {
		return nil
	}
	if !s.GPUAccess() {
		panic("slice not accessible by GPU")
	}
	return s.ptrs[component]
}

// Slice returns a slice sharing memory with the original.
// Beware that it may contain less elements than would be expected from Mesh().NCell().
func (s *Slice) Slice(a, b int) *Slice {
	len_ := int(s.len_)
	if a >= len_ || b > len_ || a > b || a < 0 || b < 0 {
		log.Panicf("slice range out of bounds: [%v:%v] (len=%v)", a, b, len_)
	}

	slice := &Slice{memType: s.memType, size: s.size}
	slice.ptrs = slice.ptr_[:s.NComp()]
	for i := range s.ptrs {
		slice.ptrs[i] = unsafe.Pointer(uintptr(s.ptrs[i]) + SIZEOF_FLOAT32*uintptr(a))
	}
	slice.len_ = b - a
	return slice
}

const SIZEOF_FLOAT32 = 4

// Host returns the Slice as a [][]float32 indexed by component, cell number.
// It should have CPUAccess() == true.
func (s *Slice) Host() [][]float32 {
	if !s.CPUAccess() {
		log.Panic("slice not accessible by CPU")
	}
	list := make([][]float32, s.NComp())
	for c := range list {
		hdr := (*reflect.SliceHeader)(unsafe.Pointer(&list[c]))
		hdr.Data = uintptr(s.ptrs[c])
		hdr.Len = int(s.len_)
		hdr.Cap = hdr.Len
	}
	return list
}

// Returns a copy of the Slice, allocated on CPU.
func (s *Slice) HostCopy() *Slice {
	cpy := NewSlice(s.NComp(), s.Size())
	// make it work if s is a part of a bigger slice:
	if cpy.Len() != s.Len() {
		cpy = cpy.Slice(0, s.Len())
	}
	Copy(cpy, s)
	util.Assert(s.ptrs[0] != nil) // todo rm
	return cpy
}

func Copy(dst, src *Slice) {
	if dst.NComp() != src.NComp() || dst.Len() != src.Len() {
		log.Panicf("slice copy: illegal sizes: dst: %vx%v, src: %vx%v", dst.NComp(), dst.Len(), src.NComp(), src.Len())
	}
	d, s := dst.GPUAccess(), src.GPUAccess()
	bytes := SIZEOF_FLOAT32 * int64(dst.Len())
	switch {
	default:
		panic("bug")
	case d && s:
		for c := 0; c < dst.NComp(); c++ {
			memCpy(dst.DevPtr(c), src.DevPtr(c), bytes)
		}
	case s && !d:
		for c := 0; c < dst.NComp(); c++ {
			memCpyDtoH(dst.ptr_[c], src.DevPtr(c), bytes)
		}
	case !s && d:
		for c := 0; c < dst.NComp(); c++ {
			memCpyHtoD(dst.DevPtr(c), src.ptr_[c], bytes)
		}
	case !d && !s:
		dst, src := dst.Host(), src.Host()
		for c := range dst {
			copy(dst[c], src[c])
		}
	}
}

// Floats returns the data as 3D array,
// indexed by cell position. Data should be
// scalar (1 component) and have CPUAccess() == true.
func (f *Slice) Scalars() [][][]float32 {
	x := f.Tensors()
	if len(x) != 1 {
		log.Panicf("expecting 1 component, got %v", f.NComp())
	}
	return x[0]
}

// Vectors returns the data as 4D array,
// indexed by component, cell position. Data should have
// 3 components and have CPUAccess() == true.
func (f *Slice) Vectors() [3][][][]float32 {
	x := f.Tensors()
	if len(x) != 3 {
		log.Panicf("expecting 3 components, got %v", f.NComp())
	}
	return [3][][][]float32{x[0], x[1], x[2]}
}

// Tensors returns the data as 4D array,
// indexed by component, cell position.
// Requires CPUAccess() == true.
func (f *Slice) Tensors() [][][][]float32 {
	tensors := make([][][][]float32, f.NComp())
	host := f.Host()
	for i := range tensors {
		tensors[i] = reshape(host[i], f.Size())
	}
	return tensors
}

// IsNil returns true if either s is nil or s.pointer[0] == nil
func (s *Slice) IsNil() bool {
	if s == nil {
		return true
	}
	return s.ptr_[0] == nil
}

func (s *Slice) String() string {
	if s == nil {
		return "nil"
	}
	var buf bytes.Buffer
	util.Fprint(&buf, s.Tensors())
	return buf.String()
}

func (s *Slice) Set(comp, ix, iy, iz int, value float64) {
	s.checkComp(comp)
	s.Host()[comp][s.Index(ix, iy, iz)] = float32(value)
}

func (s *Slice) Get(comp, ix, iy, iz int) float64 {
	s.checkComp(comp)
	return float64(s.Host()[comp][s.Index(ix, iy, iz)])
}

func (s *Slice) checkComp(comp int) {
	if comp < 0 || comp >= s.NComp() {
		log.Panicf("slice: invalid component index: %v (number of components=%v)\n", comp, s.NComp())
	}
}

func (s *Slice) Index(ix, iy, iz int) int {
	Nx := s.Size()[X]
	Ny := s.Size()[Y]
	Nz := s.Size()[Z]
	if ix < 0 || ix >= Nx || iy < 0 || iy >= Ny || iz < 0 || iz >= Nz {
		log.Panicf("Slice index out of bounds: %v,%v,%v (bounds=%v,%v,%v)\n", ix, iy, iz, Nx, Ny, Nz)
	}
	return (iz*Ny+iy)*Nx + ix
}

const (
	X = 0
	Y = 1
	Z = 2
)
