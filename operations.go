package iz68000

// The fields of the opcode word used by most instructions
func eaMode(ir uint16) int  { return int(ir>>3) & 7 }
func eaReg(ir uint16) int   { return int(ir) & 7 }
func opReg(ir uint16) int   { return int(ir>>9) & 7 }
func dstMode(ir uint16) int { return int(ir>>6) & 7 }
func dstReg(ir uint16) int  { return int(ir>>9) & 7 }

func boolBit(v bool) uint32 {
	if v {
		return 1
	}
	return 0
}

/*
The arithmetic primitives. They update the flags as a side effect, the callers
that must not change some of them save and restore the value.
*/

// doAdd returns a+b+extend updating X, N, Z, V and C
func (s *State) doAdd(a uint32, b uint32, extend uint32, size int) uint32 {
	mask := maskOf(size)
	msb := msbOf(size)

	total := uint64(a&mask) + uint64(b&mask) + uint64(extend)
	result := uint32(total) & mask

	carry := total&(uint64(mask)+1) != 0
	s.reg.updateFlag(flagC, carry)
	s.reg.updateFlag(flagX, carry)
	s.reg.updateFlag(flagV, ^(a^b)&(a^result)&msb != 0)
	s.reg.updateFlagsZN(result, size)
	return result
}

// doSub returns a-b-extend updating X, N, Z, V and C
func (s *State) doSub(a uint32, b uint32, extend uint32, size int) uint32 {
	result := s.doCompare(a, b, extend, size)
	s.reg.updateFlag(flagX, s.reg.getFlag(flagC))
	return result
}

// doCompare is doSub without updating X, as needed by CMP
func (s *State) doCompare(a uint32, b uint32, extend uint32, size int) uint32 {
	mask := maskOf(size)
	msb := msbOf(size)

	total := uint64(a&mask) - uint64(b&mask) - uint64(extend)
	result := uint32(total) & mask

	s.reg.updateFlag(flagC, total&(uint64(mask)+1) != 0)
	s.reg.updateFlag(flagV, (a^b)&(a^result)&msb != 0)
	s.reg.updateFlagsZN(result, size)
	return result
}

func (s *State) requireSupervisor() {
	if !s.reg.isSupervisor() {
		s.raiseException(vectorPrivilegeViolation)
	}
}

/*
Data movement
*/

func buildOpMove(size int) opFunc {
	return func(s *State, ir uint16, op *opcode) {
		// The extension words of the source come before the ones of the
		// destination, so the source has to be resolved first
		src := s.resolveOperand(eaMode(ir), eaReg(ir), size)
		value := s.readOperand(&src, size)

		// The flags are committed before the transfer, so an address error on
		// the destination leaves them already set. The exception is a long
		// coming from a register, there the flags are set on the same cycles
		// that write the destination.
		//
		// Note 3 of harteSuite_test.go: this is only an approximation, the
		// real ordering depends on which half of the long is written first
		// and that depends on the destination addressing mode.
		early := size != sizeLong || src.mode != modeDataRegister && src.mode != modeAddressRegister
		if early {
			s.reg.updateFlagsLogic(value, size)
		}

		dst := s.resolveOperand(dstMode(ir), dstReg(ir), size)
		s.writeOperand(&dst, size, value)

		if !early {
			s.reg.updateFlagsLogic(value, size)
		}
	}
}

func buildOpMoveA(size int) opFunc {
	return func(s *State, ir uint16, op *opcode) {
		src := s.resolveOperand(eaMode(ir), eaReg(ir), size)
		value := s.readOperand(&src, size)
		// The word variant sign extends, and MOVEA never touches the flags
		s.reg.setA(dstReg(ir), signExtend(value, size))
	}
}

func opMOVEQ(s *State, ir uint16, op *opcode) {
	value := signExtend(uint32(uint8(ir)), sizeByte)
	s.reg.setDSized(opReg(ir), sizeLong, value)
	s.reg.updateFlagsLogic(value, sizeLong)
}

func opLEA(s *State, ir uint16, op *opcode) {
	src := s.resolveOperand(eaMode(ir), eaReg(ir), sizeLong)
	s.reg.setA(opReg(ir), src.address)
}

func opPEA(s *State, ir uint16, op *opcode) {
	src := s.resolveOperand(eaMode(ir), eaReg(ir), sizeLong)
	s.push(src.address, sizeLong)
}

func buildOpExt(size int) opFunc {
	return func(s *State, ir uint16, op *opcode) {
		// EXT.W extends a byte, EXT.L extends a word
		value := signExtend(s.reg.getDSized(eaReg(ir), size/2), size/2)
		s.reg.setDSized(eaReg(ir), size, value)
		s.reg.updateFlagsLogic(value, size)
	}
}

func opSWAP(s *State, ir uint16, op *opcode) {
	value := s.reg.getD(eaReg(ir))
	value = value>>16 | value<<16
	s.reg.setDSized(eaReg(ir), sizeLong, value)
	s.reg.updateFlagsLogic(value, sizeLong)
}

/*
Arithmetic and logic
*/

type aluFunc func(s *State, dst uint32, src uint32, size int) uint32

func aluOr(s *State, dst uint32, src uint32, size int) uint32 {
	result := dst | src
	s.reg.updateFlagsLogic(result, size)
	return result
}

func aluAnd(s *State, dst uint32, src uint32, size int) uint32 {
	result := dst & src
	s.reg.updateFlagsLogic(result, size)
	return result
}

func aluEor(s *State, dst uint32, src uint32, size int) uint32 {
	result := dst ^ src
	s.reg.updateFlagsLogic(result, size)
	return result
}

func aluAdd(s *State, dst uint32, src uint32, size int) uint32 {
	return s.doAdd(dst, src, 0, size)
}

func aluSub(s *State, dst uint32, src uint32, size int) uint32 {
	return s.doSub(dst, src, 0, size)
}

// aluCmp does not write back the result, the callers discard it
func aluCmp(s *State, dst uint32, src uint32, size int) uint32 {
	s.doCompare(dst, src, 0, size)
	return dst
}

// buildOpAluImmediate implements ORI, ANDI, SUBI, ADDI, EORI and CMPI
func buildOpAluImmediate(alu aluFunc, writes bool, size int) opFunc {
	return func(s *State, ir uint16, op *opcode) {
		// The immediate comes before the extension words of the destination
		src := s.resolveOperand(modeExtended, modeExtImmediate, size)
		value := s.readOperand(&src, size)

		dst := s.resolveOperand(eaMode(ir), eaReg(ir), size)
		result := alu(s, s.readOperand(&dst, size), value, size)
		if writes {
			s.writeOperand(&dst, size, result)
		}
	}
}

// buildOpAluToRegister implements the <ea> op Dn -> Dn direction
func buildOpAluToRegister(alu aluFunc, writes bool, size int) opFunc {
	return func(s *State, ir uint16, op *opcode) {
		src := s.resolveOperand(eaMode(ir), eaReg(ir), size)
		value := s.readOperand(&src, size)

		reg := opReg(ir)
		result := alu(s, s.reg.getDSized(reg, size), value, size)
		if writes {
			s.reg.setDSized(reg, size, result)
		}
	}
}

// buildOpAluToMemory implements the Dn op <ea> -> <ea> direction
func buildOpAluToMemory(alu aluFunc, size int) opFunc {
	return func(s *State, ir uint16, op *opcode) {
		dst := s.resolveOperand(eaMode(ir), eaReg(ir), size)
		value := s.reg.getDSized(opReg(ir), size)
		result := alu(s, s.readOperand(&dst, size), value, size)
		s.writeOperand(&dst, size, result)
	}
}

// buildOpAluAddress implements ADDA, SUBA and CMPA. The source is sign
// extended to a long and the flags are only changed by CMPA.
func buildOpAluAddress(alu aluFunc, writes bool, size int) opFunc {
	return func(s *State, ir uint16, op *opcode) {
		src := s.resolveOperand(eaMode(ir), eaReg(ir), size)
		value := signExtend(s.readOperand(&src, size), size)

		reg := opReg(ir)
		if !writes {
			// CMPA always compares the full long
			aluCmp(s, s.reg.getA(reg), value, sizeLong)
			return
		}

		sr := s.reg.getSR()
		result := alu(s, s.reg.getA(reg), value, sizeLong)
		s.reg.sr = sr // ADDA and SUBA don't change the flags
		s.reg.setA(reg, result)
	}
}

// buildOpQuick implements ADDQ and SUBQ. The data of 1 to 8 is encoded on the
// opcode, with 0 meaning 8.
func buildOpQuick(alu aluFunc, size int) opFunc {
	return func(s *State, ir uint16, op *opcode) {
		value := uint32(opReg(ir))
		if value == 0 {
			value = 8
		}

		mode := eaMode(ir)
		dst := s.resolveOperand(mode, eaReg(ir), size)
		if mode == modeAddressRegister {
			// The whole address register is used and the flags are preserved
			sr := s.reg.getSR()
			result := alu(s, s.reg.getA(eaReg(ir)), value, sizeLong)
			s.reg.sr = sr
			s.reg.setA(eaReg(ir), result)
			return
		}

		result := alu(s, s.readOperand(&dst, size), value, size)
		s.writeOperand(&dst, size, result)
	}
}

func buildOpCLR(size int) opFunc {
	return func(s *State, ir uint16, op *opcode) {
		dst := s.resolveOperand(eaMode(ir), eaReg(ir), size)
		// The 68000 reads the operand before clearing it, a quirk that is
		// visible on the hardware registers with side effects on read
		s.readOperand(&dst, size)
		s.reg.setFlag(flagZ)
		s.reg.clearFlag(flagN | flagV | flagC)

		s.writeOperand(&dst, size, 0)
	}
}

func buildOpNEG(size int) opFunc {
	return func(s *State, ir uint16, op *opcode) {
		dst := s.resolveOperand(eaMode(ir), eaReg(ir), size)
		result := s.doSub(0, s.readOperand(&dst, size), 0, size)
		s.writeOperand(&dst, size, result)
	}
}

func buildOpNEGX(size int) opFunc {
	return func(s *State, ir uint16, op *opcode) {
		dst := s.resolveOperand(eaMode(ir), eaReg(ir), size)

		// Z is only cleared, never set, so that it reflects the result of a
		// whole multiprecision operation
		zero := s.reg.getFlag(flagZ)
		result := s.doSub(0, s.readOperand(&dst, size), s.reg.getFlagBit(flagX), size)
		s.reg.updateFlag(flagZ, zero && result == 0)

		s.writeOperand(&dst, size, result)
	}
}

func buildOpNOT(size int) opFunc {
	return func(s *State, ir uint16, op *opcode) {
		dst := s.resolveOperand(eaMode(ir), eaReg(ir), size)
		result := ^s.readOperand(&dst, size) & maskOf(size)
		s.reg.updateFlagsLogic(result, size)
		s.writeOperand(&dst, size, result)
	}
}

func buildOpTST(size int) opFunc {
	return func(s *State, ir uint16, op *opcode) {
		src := s.resolveOperand(eaMode(ir), eaReg(ir), size)
		s.reg.updateFlagsLogic(s.readOperand(&src, size), size)
	}
}

/*
Bit manipulation. The bit number is taken modulo 32 on the data registers and
modulo 8 on the memory operands, that are always accessed as bytes.
*/

const (
	bitTest = iota
	bitChange
	bitClear
	bitSet
)

func buildOpBit(operation int, static bool) opFunc {
	return func(s *State, ir uint16, op *opcode) {
		var bit uint32
		if static {
			bit = uint32(s.fetchWord())
		} else {
			bit = s.reg.getD(opReg(ir))
		}

		mode := eaMode(ir)
		size := sizeByte
		if mode == modeDataRegister {
			size = sizeLong
		}
		bit &= uint32(size)*8 - 1

		if !static && operation != bitTest && size == sizeLong && bit > 15 {
			// Reaching the high half of a data register takes two more cycles.
			// The static variants have it folded on their base time.
			s.extraCycles += 2
		}

		dst := s.resolveOperand(mode, eaReg(ir), size)
		value := s.readOperand(&dst, size)
		s.reg.updateFlag(flagZ, value&(1<<bit) == 0)

		switch operation {
		case bitChange:
			s.writeOperand(&dst, size, value^(1<<bit))
		case bitClear:
			s.writeOperand(&dst, size, value&^(1<<bit))
		case bitSet:
			s.writeOperand(&dst, size, value|(1<<bit))
		}
	}
}

/*
Shifts and rotates
*/

const (
	shiftArithmetic = iota
	shiftLogical
	shiftRotateExtend
	shiftRotate
)

// doShift applies count iterations of a shift or rotate updating the flags.
// It is done one bit at a time because the V flag of ASL is set if the sign
// changes at any point, not only at the end.
func (s *State) doShift(value uint32, count int, size int, kind int, left bool) uint32 {
	mask := maskOf(size)
	msb := msbOf(size)
	value &= mask

	if count == 0 {
		// Only possible when the count comes from a register
		if kind == shiftRotateExtend {
			s.reg.updateFlag(flagC, s.reg.getFlag(flagX))
		} else {
			s.reg.clearFlag(flagC)
		}
		s.reg.clearFlag(flagV)
		s.reg.updateFlagsZN(value, size)
		return value
	}

	carry := false
	overflow := false
	for i := 0; i < count; i++ {
		if left {
			carry = value&msb != 0
			previous := value
			value = value << 1 & mask
			switch kind {
			case shiftRotate:
				value |= boolBit(carry)
			case shiftRotateExtend:
				value |= s.reg.getFlagBit(flagX)
				s.reg.updateFlag(flagX, carry)
			}
			if kind == shiftArithmetic && (previous^value)&msb != 0 {
				overflow = true
			}
		} else {
			carry = value&1 != 0
			sign := value & msb
			value >>= 1
			switch kind {
			case shiftArithmetic:
				value |= sign
			case shiftRotate:
				value |= boolBit(carry) * msb
			case shiftRotateExtend:
				value |= s.reg.getFlagBit(flagX) * msb
				s.reg.updateFlag(flagX, carry)
			}
		}
	}

	s.reg.updateFlag(flagC, carry)
	if kind == shiftArithmetic || kind == shiftLogical {
		s.reg.updateFlag(flagX, carry)
	}
	s.reg.updateFlag(flagV, overflow)
	s.reg.updateFlagsZN(value, size)
	return value
}

// buildOpShiftRegister shifts a data register by an immediate count of 1 to 8
// or by the count on another data register
func buildOpShiftRegister(kind int, left bool, size int) opFunc {
	return func(s *State, ir uint16, op *opcode) {
		var count int
		if ir&0x0020 == 0 {
			count = opReg(ir)
			if count == 0 {
				count = 8
			}
		} else {
			count = int(s.reg.getD(opReg(ir)) & 63)
		}

		reg := eaReg(ir)
		result := s.doShift(s.reg.getDSized(reg, size), count, size, kind, left)
		s.reg.setDSized(reg, size, result)
		s.extraCycles = 2 * count
	}
}

// buildOpShiftMemory shifts a word in memory by one bit
func buildOpShiftMemory(kind int, left bool) opFunc {
	return func(s *State, ir uint16, op *opcode) {
		dst := s.resolveOperand(eaMode(ir), eaReg(ir), sizeWord)
		result := s.doShift(s.readOperand(&dst, sizeWord), 1, sizeWord, kind, left)
		s.writeOperand(&dst, sizeWord, result)
	}
}

/*
Program control
*/

// The condition codes as encoded on the 4 bit condition field
const (
	condT = iota
	condF
	condHI
	condLS
	condCC
	condCS
	condNE
	condEQ
	condVC
	condVS
	condPL
	condMI
	condGE
	condLT
	condGT
	condLE
)

var conditionNames = [16]string{
	"T", "F", "HI", "LS", "CC", "CS", "NE", "EQ",
	"VC", "VS", "PL", "MI", "GE", "LT", "GT", "LE",
}

func (s *State) testCondition(condition int) bool {
	c := s.reg.getFlag(flagC)
	v := s.reg.getFlag(flagV)
	z := s.reg.getFlag(flagZ)
	n := s.reg.getFlag(flagN)

	switch condition {
	case condT:
		return true
	case condF:
		return false
	case condHI:
		return !c && !z
	case condLS:
		return c || z
	case condCC:
		return !c
	case condCS:
		return c
	case condNE:
		return !z
	case condEQ:
		return z
	case condVC:
		return !v
	case condVS:
		return v
	case condPL:
		return !n
	case condMI:
		return n
	case condGE:
		return n == v
	case condLT:
		return n != v
	case condGT:
		return n == v && !z
	case condLE:
		return n != v || z
	}
	return false
}

// fetchDisplacement returns the branch target. A zero byte displacement means
// that a word displacement follows the opcode.
func (s *State) fetchDisplacement(ir uint16) uint32 {
	base := s.reg.getPC()
	if uint8(ir) == 0 {
		return base + signExtend(uint32(s.fetchWord()), sizeWord)
	}
	return base + signExtend(uint32(ir), sizeByte)
}

// buildOpBranch implements Bcc and BRA. The base time on the table is the one
// of a not taken branch with a byte displacement.
func buildOpBranch(condition int) opFunc {
	return func(s *State, ir uint16, op *opcode) {
		wordDisplacement := uint8(ir) == 0
		target := s.fetchDisplacement(ir)

		if s.testCondition(condition) {
			s.extraCycles = 2
			s.jump(target)
		} else if wordDisplacement {
			s.extraCycles = 4
		}
	}
}

func opBSR(s *State, ir uint16, op *opcode) {
	target := s.fetchDisplacement(ir)
	s.push(s.reg.getPC(), sizeLong)
	s.jump(target)
}

// buildOpDBcc decrements the counter and branches while the condition is
// false, until the counter underflows from 0 to -1
func buildOpDBcc(condition int) opFunc {
	return func(s *State, ir uint16, op *opcode) {
		base := s.reg.getPC()
		displacement := signExtend(uint32(s.fetchWord()), sizeWord)

		if s.testCondition(condition) {
			s.extraCycles = 2
			return
		}

		reg := eaReg(ir)
		counter := (s.reg.getDSized(reg, sizeWord) - 1) & 0xffff
		if counter == 0xffff {
			// The loop is over, the counter underflows to -1
			s.reg.setDSized(reg, sizeWord, counter)
			s.extraCycles = 4
			return
		}

		// The counter is written back only once the target is known to be
		// good, an address error there leaves it untouched
		s.checkJumpTarget(base + displacement)
		s.reg.setDSized(reg, sizeWord, counter)
		s.jump(base + displacement)
	}
}

func buildOpScc(condition int) opFunc {
	return func(s *State, ir uint16, op *opcode) {
		dst := s.resolveOperand(eaMode(ir), eaReg(ir), sizeByte)
		if s.testCondition(condition) {
			s.writeOperand(&dst, sizeByte, 0xff)
			if dst.mode == modeDataRegister {
				s.extraCycles = 2
			}
		} else {
			s.writeOperand(&dst, sizeByte, 0x00)
		}
	}
}

func opJMP(s *State, ir uint16, op *opcode) {
	dst := s.resolveOperand(eaMode(ir), eaReg(ir), sizeLong)
	s.jump(dst.address)
}

func opJSR(s *State, ir uint16, op *opcode) {
	// The address is resolved before pushing, the return address is the one
	// after the extension words. An odd target is detected before pushing, it
	// must leave the stack pointer where it was.
	dst := s.resolveOperand(eaMode(ir), eaReg(ir), sizeLong)
	if dst.address&1 != 0 {
		// Detected before pushing, so the instruction is aborted rather than
		// completed like on the other jumps
		s.reg.setPC(dst.address)
		s.raiseAddressError(dst.address, false)
	}
	s.push(s.reg.getPC(), sizeLong)
	s.jump(dst.address)
}

func opRTS(s *State, ir uint16, op *opcode) {
	s.jump(s.pull(sizeLong))
}

func opRTR(s *State, ir uint16, op *opcode) {
	s.reg.setCCR(uint8(s.pull(sizeWord)))
	s.jump(s.pull(sizeLong))
}

func opRTE(s *State, ir uint16, op *opcode) {
	s.requireSupervisor()
	sr := s.pull(sizeWord)
	pc := s.pull(sizeLong)
	s.reg.setSR(uint16(sr))
	s.jump(pc)
}

func opLINK(s *State, ir uint16, op *opcode) {
	displacement := signExtend(uint32(s.fetchWord()), sizeWord)
	reg := eaReg(ir)
	s.push(s.reg.getA(reg), sizeLong)
	s.reg.setA(reg, s.reg.getSP())
	s.reg.setSP(s.reg.getSP() + displacement)
}

func opUNLK(s *State, ir uint16, op *opcode) {
	// The long is read before moving the stack pointer, so that an odd
	// address register leaves the stack untouched for the exception frame
	reg := eaReg(ir)
	address := s.reg.getA(reg)
	value := s.peekLong(address)
	s.reg.setSP(address + sizeLong)
	s.reg.setA(reg, value)
}

func opTRAP(s *State, ir uint16, op *opcode) {
	s.raiseException(vectorTrap + int(ir&0xf))
}

func opTRAPV(s *State, ir uint16, op *opcode) {
	if s.reg.getFlag(flagV) {
		s.raiseException(vectorTrapv)
	}
}

func opNOP(s *State, ir uint16, op *opcode) {
}

func opRESET(s *State, ir uint16, op *opcode) {
	s.requireSupervisor()
	// The external devices are reset, the processor state is not affected
	if line, ok := s.mem.(ResetLine); ok {
		line.ResetDevices()
	}
}

func opSTOP(s *State, ir uint16, op *opcode) {
	s.requireSupervisor()
	s.reg.setSR(s.fetchWord())
	s.stopped = true
}

func opILLEGAL(s *State, ir uint16, op *opcode) {
	s.raiseException(vectorIllegalInstruction)
}

// opLineA handles the unimplemented instructions starting with $A. The
// Macintosh uses them for all the toolbox and operating system calls.
func opLineA(s *State, ir uint16, op *opcode) {
	s.raiseException(vectorLineA)
}

func opLineF(s *State, ir uint16, op *opcode) {
	s.raiseException(vectorLineF)
}

/*
Adapters to leave on the opcode table only the parameters of each family. The
size is the last one to be bound, the table applies it when expanding the
patterns.
*/

func buildAluImmediate(alu aluFunc, writes bool) func(int) opFunc {
	return func(size int) opFunc { return buildOpAluImmediate(alu, writes, size) }
}

func buildAluToRegister(alu aluFunc, writes bool) func(int) opFunc {
	return func(size int) opFunc { return buildOpAluToRegister(alu, writes, size) }
}

func buildAluToMemory(alu aluFunc) func(int) opFunc {
	return func(size int) opFunc { return buildOpAluToMemory(alu, size) }
}

func buildAluAddress(alu aluFunc, writes bool) func(int) opFunc {
	return func(size int) opFunc { return buildOpAluAddress(alu, writes, size) }
}

func buildQuick(alu aluFunc) func(int) opFunc {
	return func(size int) opFunc { return buildOpQuick(alu, size) }
}

func buildShiftRegister(kind int, left bool) func(int) opFunc {
	return func(size int) opFunc { return buildOpShiftRegister(kind, left, size) }
}
