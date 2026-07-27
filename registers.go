package iz68000

import "fmt"

// The 16 registers of the 68000. The address registers are stored after the
// data registers so that the 4 bit register selectors, as used by MOVEM or
// EXG, can index them directly.
const (
	regD0 = 0
	regA0 = 8
	regA7 = 15

	regCount = 16
)

// The sizes of the operations, in bytes. The value is used to build the masks.
const (
	sizeByte = 1
	sizeWord = 2
	sizeLong = 4
)

const (
	flagT uint16 = 1 << 15 // Trace
	flagS uint16 = 1 << 13 // Supervisor
	flagI uint16 = 7 << 8  // Interrupt mask
	flagX uint16 = 1 << 4  // Extend
	flagN uint16 = 1 << 3  // Negative
	flagZ uint16 = 1 << 2  // Zero
	flagV uint16 = 1 << 1  // Overflow
	flagC uint16 = 1 << 0  // Carry

	// Bits 14, 12, 11, 7, 6 and 5 are unused on the 68000 and always read as 0
	srMask  uint16 = flagT | flagS | flagI | ccrMask
	ccrMask uint16 = flagX | flagN | flagZ | flagV | flagC

	interruptMaskShift = 8
)

type registers struct {
	data [regCount]uint32
	pc   uint32
	sr   uint16

	// The 68000 has two stack pointers, only one of them mapped on A7 at a
	// time. The one not selected by the S flag is kept here.
	otherSP uint32
}

func (r *registers) getRegister(i int) uint32    { return r.data[i] }
func (r *registers) setRegister(i int, v uint32) { r.data[i] = v }
func (r *registers) getD(i int) uint32           { return r.data[regD0+i] }
func (r *registers) getA(i int) uint32           { return r.data[regA0+i] }
func (r *registers) setA(i int, v uint32)        { r.data[regA0+i] = v }
func (r *registers) getSP() uint32               { return r.data[regA7] }
func (r *registers) setSP(v uint32)              { r.data[regA7] = v }
func (r *registers) getPC() uint32               { return r.pc }
func (r *registers) setPC(v uint32)              { r.pc = v }
func (r *registers) getSR() uint16               { return r.sr }
func (r *registers) getCCR() uint8               { return uint8(r.sr & ccrMask) }
func (r *registers) isSupervisor() bool          { return r.sr&flagS != 0 }
func (r *registers) getInterruptMask() uint8     { return uint8((r.sr & flagI) >> interruptMaskShift) }
func (r *registers) setInterruptMask(level uint8) {
	r.sr = r.sr&^flagI | uint16(level)<<interruptMaskShift
}

// getDSized returns the low byte, word or long of a data register
func (r *registers) getDSized(i int, size int) uint32 {
	return r.data[regD0+i] & maskOf(size)
}

// setDSized changes the low byte, word or long of a data register. The rest of
// the register is preserved, only long operations write the full 32 bits.
func (r *registers) setDSized(i int, size int, v uint32) {
	mask := maskOf(size)
	r.data[regD0+i] = r.data[regD0+i]&^mask | v&mask
}

// setSR changes the status register. The stack pointers are swapped if the
// supervisor state changes, as A7 maps to a different register on each mode.
func (r *registers) setSR(v uint16) {
	wasSupervisor := r.isSupervisor()
	r.sr = v & srMask
	if r.isSupervisor() != wasSupervisor {
		r.data[regA7], r.otherSP = r.otherSP, r.data[regA7]
	}
}

func (r *registers) setCCR(v uint8) {
	r.sr = r.sr&^ccrMask | uint16(v)&ccrMask
}

// getUSP returns the user stack pointer, wherever it is stored
func (r *registers) getUSP() uint32 {
	if r.isSupervisor() {
		return r.otherSP
	}
	return r.data[regA7]
}

func (r *registers) setUSP(v uint32) {
	if r.isSupervisor() {
		r.otherSP = v
	} else {
		r.data[regA7] = v
	}
}

func (r *registers) getFlagBit(flag uint16) uint32 {
	if r.getFlag(flag) {
		return 1
	}
	return 0
}

func (r *registers) getFlag(flag uint16) bool {
	return r.sr&flag != 0
}

func (r *registers) setFlag(flag uint16) {
	r.sr |= flag
}

func (r *registers) clearFlag(flag uint16) {
	r.sr &^= flag
}

func (r *registers) updateFlag(flag uint16, v bool) {
	if v {
		r.setFlag(flag)
	} else {
		r.clearFlag(flag)
	}
}

// updateFlagsZN is the most common flag update: Z and N from the result
func (r *registers) updateFlagsZN(value uint32, size int) {
	r.updateFlag(flagZ, value&maskOf(size) == 0)
	r.updateFlag(flagN, value&msbOf(size) != 0)
}

// updateFlagsLogic is used by the operations that also clear V and C, but
// leave X untouched: MOVE, AND, OR, EOR, NOT, TST, EXT and SWAP
func (r *registers) updateFlagsLogic(value uint32, size int) {
	r.updateFlagsZN(value, size)
	r.clearFlag(flagV | flagC)
}

// maskOf returns the bits used by an operation of the given size
func maskOf(size int) uint32 {
	switch size {
	case sizeByte:
		return 0x000000ff
	case sizeWord:
		return 0x0000ffff
	}
	return 0xffffffff
}

// msbOf returns the sign bit of an operation of the given size
func msbOf(size int) uint32 {
	return 1 << (size*8 - 1)
}

// signExtend converts a byte or word to the equivalent negative long
func signExtend(value uint32, size int) uint32 {
	switch size {
	case sizeByte:
		return uint32(int32(int8(value)))
	case sizeWord:
		return uint32(int32(int16(value)))
	}
	return value
}

func sizeName(size int) string {
	switch size {
	case sizeByte:
		return "B"
	case sizeWord:
		return "W"
	case sizeLong:
		return "L"
	}
	return "?"
}

func (r registers) String() string {
	result := "D:"
	for i := 0; i < 8; i++ {
		result += fmt.Sprintf(" %08x", r.getD(i))
	}
	result += ", A:"
	for i := 0; i < 8; i++ {
		result += fmt.Sprintf(" %08x", r.getA(i))
	}
	return result + fmt.Sprintf(", PC: %06x, SR: %04x (%s)", r.pc, r.sr, r.flagsString())
}

func (r registers) flagsString() string {
	names := []struct {
		flag uint16
		name byte
	}{
		{flagT, 'T'}, {flagS, 'S'}, {flagX, 'X'},
		{flagN, 'N'}, {flagZ, 'Z'}, {flagV, 'V'}, {flagC, 'C'},
	}
	result := make([]byte, 0, len(names)+1)
	for _, f := range names {
		if r.getFlag(f.flag) {
			result = append(result, f.name)
		} else {
			result = append(result, '-')
		}
	}
	return string(append(result, '0'+r.getInterruptMask()))
}
