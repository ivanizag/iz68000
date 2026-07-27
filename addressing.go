package iz68000

import "fmt"

// The addressing modes as encoded on the 3 bit mode field of the opcodes
const (
	modeDataRegister          = iota // Dn
	modeAddressRegister              // An
	modeIndirect                     // (An)
	modeIndirectPostincrement        // (An)+
	modeIndirectPredecrement         // -(An)
	modeIndirectDisplacement         // (d16,An)
	modeIndirectIndexed              // (d8,An,Xn)
	modeExtended                     // The register field selects the mode
)

// When the mode field is modeExtended, the register field completes it
const (
	modeExtAbsoluteShort  = iota // (xxx).W
	modeExtAbsoluteLong          // (xxx).L
	modeExtPCDisplacement        // (d16,PC)
	modeExtPCIndexed             // (d8,PC,Xn)
	modeExtImmediate             // #<data>
)

// The groups of addressing modes accepted by each instruction, as defined on
// the "Effective Addressing Modes and Categories" table of the manual
const (
	eaNone = iota
	eaAll
	eaData             // All but An
	eaMemory           // All but Dn and An
	eaControl          // Memory, but not postincrement, predecrement or immediate
	eaAlterable        // All but the PC relative modes and immediate
	eaDataAlterable    // Data and alterable
	eaMemoryAlterable  // Memory and alterable
	eaControlAlterable // Control and alterable

	// The two combinations used only by MOVEM
	eaMovemToMemory   // Control alterable plus predecrement
	eaMovemToRegister // Control plus postincrement
)

// operand is the result of resolving an effective address. The address of the
// memory modes is resolved once, so that the read-modify-write instructions
// don't repeat the side effects of the postincrement and predecrement modes.
type operand struct {
	mode    int
	reg     int
	address uint32 // For the memory modes
	value   uint32 // For the immediate mode

	// The increment and decrement modes update the address register, but not
	// always at the same point of the transfer. The update is left pending
	// here and committed by readOperand() or writeOperand().
	pending      bool
	pendingValue uint32
}

// resolveOperand decodes an effective address, consuming the extension words
// that follow the opcode. It must be called once per operand and in the order
// the operands appear on the instruction.
func (s *State) resolveOperand(mode int, reg int, size int) operand {
	o := operand{mode: mode, reg: reg}

	switch mode {
	case modeDataRegister, modeAddressRegister:
		// The value is on a register, there is no address to resolve
	case modeIndirect:
		o.address = s.reg.getA(reg)
	case modeIndirectPostincrement:
		o.address = s.reg.getA(reg)
		o.pending, o.pendingValue = true, o.address+stackAdjust(reg, size)
	case modeIndirectPredecrement:
		// The decrement is part of the address calculation and is committed
		// right away, even if the transfer then raises an address error
		o.address = s.reg.getA(reg) - stackAdjust(reg, size)
		s.reg.setA(reg, o.address)
	case modeIndirectDisplacement:
		o.address = s.reg.getA(reg) + signExtend(uint32(s.fetchWord()), sizeWord)
	case modeIndirectIndexed:
		o.address = s.resolveIndex(s.reg.getA(reg))
	case modeExtended:
		switch reg {
		case modeExtAbsoluteShort:
			o.address = signExtend(uint32(s.fetchWord()), sizeWord)
		case modeExtAbsoluteLong:
			o.address = s.fetchLong()
		case modeExtPCDisplacement:
			// The displacement is relative to the extension word itself
			base := s.reg.getPC()
			o.address = base + signExtend(uint32(s.fetchWord()), sizeWord)
		case modeExtPCIndexed:
			o.address = s.resolveIndex(s.reg.getPC())
		case modeExtImmediate:
			if size == sizeLong {
				o.value = s.fetchLong()
			} else {
				// Byte immediates are stored on the low half of a word
				o.value = uint32(s.fetchWord()) & maskOf(size)
			}
		default:
			panic(fmt.Sprintf("Assert failed. Missing extended addressing mode %d", reg))
		}
	default:
		panic(fmt.Sprintf("Assert failed. Missing addressing mode %d", mode))
	}

	s.lastResolvedAddress = o.address
	return o
}

// resolveIndex applies the extension word of the indexed addressing modes
func (s *State) resolveIndex(base uint32) uint32 {
	extension := s.fetchWord()

	index := s.reg.getRegister(int(extension>>12) & 0xf)
	if extension&0x0800 == 0 {
		// The index register is used as a sign extended word
		index = signExtend(index, sizeWord)
	}

	displacement := signExtend(uint32(extension), sizeByte)
	return base + index + displacement
}

// stackAdjust returns the increment of the postincrement and predecrement
// modes. A7 is always kept even to preserve the alignment of the stack.
func stackAdjust(reg int, size int) uint32 {
	if size == sizeByte && reg == 7 {
		return 2
	}
	return uint32(size)
}

func (s *State) readOperand(o *operand, size int) uint32 {
	switch o.mode {
	case modeDataRegister:
		return s.reg.getDSized(o.reg, size)
	case modeAddressRegister:
		return s.reg.getA(o.reg) & maskOf(size)
	case modeExtended:
		if o.reg == modeExtImmediate {
			return o.value
		}
	}

	if size != sizeLong {
		// A byte or a word is read on a single bus cycle, with the address
		// register already written back when an address error aborts it. A
		// long takes two cycles and is written back only after both.
		s.commitAddressRegister(o)
	}
	value := s.peekSized(o.address, size)
	s.commitAddressRegister(o)
	return value
}

func (s *State) writeOperand(o *operand, size int, value uint32) {
	switch o.mode {
	case modeDataRegister:
		s.reg.setDSized(o.reg, size, value)
	case modeAddressRegister:
		// Address registers are never written partially
		s.reg.setA(o.reg, signExtend(value, size))
	default:
		s.pokeSized(o.address, size, value)
		s.commitAddressRegister(o)
	}
}

// commitAddressRegister applies the pending update of the increment and
// decrement modes. The read-modify-write instructions access the same operand
// twice, it must be applied only once.
func (s *State) commitAddressRegister(o *operand) {
	if o.pending {
		s.reg.setA(o.reg, o.pendingValue)
		o.pending = false
	}
}

// isValidEA tells if an addressing mode belongs to one of the categories
func isValidEA(kind int, mode int, reg int) bool {
	if mode == modeExtended && reg > modeExtImmediate {
		// Reserved encodings, they are illegal on the 68000
		return false
	}

	isData := mode != modeAddressRegister
	isMemory := mode != modeDataRegister && mode != modeAddressRegister
	isControl := isMemory &&
		mode != modeIndirectPostincrement &&
		mode != modeIndirectPredecrement &&
		!(mode == modeExtended && reg == modeExtImmediate)
	isAlterable := !(mode == modeExtended && reg >= modeExtPCDisplacement)

	switch kind {
	case eaAll:
		return true
	case eaData:
		return isData
	case eaMemory:
		return isMemory
	case eaControl:
		return isControl
	case eaAlterable:
		return isAlterable
	case eaDataAlterable:
		return isData && isAlterable
	case eaMemoryAlterable:
		return isMemory && isAlterable
	case eaControlAlterable:
		return isControl && isAlterable
	case eaMovemToMemory:
		return isControl && isAlterable || mode == modeIndirectPredecrement
	case eaMovemToRegister:
		return isControl || mode == modeIndirectPostincrement
	}
	return false
}

/*
Effective address calculation times, in cycles, from the "Effective Address
Calculation Times" table of the manual. They are added to the base time of
each instruction.
*/
func eaTime(mode int, reg int, size int) int {
	long := size == sizeLong

	switch mode {
	case modeDataRegister, modeAddressRegister:
		return 0
	case modeIndirect, modeIndirectPostincrement:
		return pick(long, 8, 4)
	case modeIndirectPredecrement:
		return pick(long, 10, 6)
	case modeIndirectDisplacement:
		return pick(long, 12, 8)
	case modeIndirectIndexed:
		return pick(long, 14, 10)
	case modeExtended:
		switch reg {
		case modeExtAbsoluteShort:
			return pick(long, 12, 8)
		case modeExtAbsoluteLong:
			return pick(long, 16, 12)
		case modeExtPCDisplacement:
			return pick(long, 12, 8)
		case modeExtPCIndexed:
			return pick(long, 14, 10)
		case modeExtImmediate:
			return pick(long, 8, 4)
		}
	}
	return 0
}

/*
eaTimeAddress is the time of LEA and PEA, that compute an address without
reading the operand and so don't pay for the transfer.
*/
func eaTimeAddress(mode int, reg int) int {
	switch mode {
	case modeIndirect:
		return 0
	case modeIndirectDisplacement:
		return 4
	case modeIndirectIndexed:
		return 8
	case modeExtended:
		switch reg {
		case modeExtAbsoluteShort, modeExtPCDisplacement:
			return 4
		case modeExtAbsoluteLong, modeExtPCIndexed:
			return 8
		}
	}
	return 0
}

/*
eaTimeJump is the time of JMP and JSR, that prefetch from the target instead
of reading an operand.
*/
func eaTimeJump(mode int, reg int) int {
	switch mode {
	case modeIndirect:
		return 0
	case modeIndirectDisplacement:
		return 2
	case modeIndirectIndexed:
		return 6
	case modeExtended:
		switch reg {
		case modeExtAbsoluteShort, modeExtPCDisplacement:
			return 2
		case modeExtAbsoluteLong:
			return 4
		case modeExtPCIndexed:
			return 6
		}
	}
	return 0
}

// eaTimeMovem is the time of MOVEM, like the address only one but two cycles
// shorter on the indexed modes
func eaTimeMovem(mode int, reg int) int {
	if mode == modeIndirectIndexed ||
		(mode == modeExtended && reg == modeExtPCIndexed) {
		return 6
	}
	return eaTimeAddress(mode, reg)
}

// eaTimeMoveDestination is the time of the destination of MOVE, that is
// shorter than the regular one for the predecrement mode
func eaTimeMoveDestination(mode int, reg int, size int) int {
	if mode == modeIndirectPredecrement {
		return pick(size == sizeLong, 8, 4)
	}
	return eaTime(mode, reg, size)
}

func pick(cond bool, whenTrue int, whenFalse int) int {
	if cond {
		return whenTrue
	}
	return whenFalse
}

// operandString disassembles an effective address, advancing pc over the
// extension words it uses
func (s *State) operandString(mode int, reg int, size int, pc *uint32) string {
	switch mode {
	case modeDataRegister:
		return fmt.Sprintf("D%d", reg)
	case modeAddressRegister:
		return fmt.Sprintf("A%d", reg)
	case modeIndirect:
		return fmt.Sprintf("(A%d)", reg)
	case modeIndirectPostincrement:
		return fmt.Sprintf("(A%d)+", reg)
	case modeIndirectPredecrement:
		return fmt.Sprintf("-(A%d)", reg)
	case modeIndirectDisplacement:
		return fmt.Sprintf("($%x,A%d)", s.nextWord(pc), reg)
	case modeIndirectIndexed:
		return s.indexString(fmt.Sprintf("A%d", reg), pc)
	case modeExtended:
		switch reg {
		case modeExtAbsoluteShort:
			return fmt.Sprintf("($%x).W", s.nextWord(pc))
		case modeExtAbsoluteLong:
			return fmt.Sprintf("($%x).L", s.nextLong(pc))
		case modeExtPCDisplacement:
			return fmt.Sprintf("($%x,PC)", s.nextWord(pc))
		case modeExtPCIndexed:
			return s.indexString("PC", pc)
		case modeExtImmediate:
			if size == sizeLong {
				return fmt.Sprintf("#$%x", s.nextLong(pc))
			}
			return fmt.Sprintf("#$%x", s.nextWord(pc)&uint16(maskOf(size)))
		}
	}
	return "???"
}

func (s *State) indexString(base string, pc *uint32) string {
	extension := s.nextWord(pc)

	register := "D"
	if extension&0x8000 != 0 {
		register = "A"
	}
	width := "W"
	if extension&0x0800 != 0 {
		width = "L"
	}

	return fmt.Sprintf("($%x,%s,%s%d.%s)", uint8(extension), base,
		register, (extension>>12)&7, width)
}

// peekCodeWord reads a word of the instruction stream without side effects,
// used only by the disassembler
func (s *State) peekCodeWord(address uint32) uint16 {
	return uint16(s.mem.PeekCode(address&addressMask))<<8 |
		uint16(s.mem.PeekCode((address+1)&addressMask))
}

func (s *State) nextWord(pc *uint32) uint16 {
	value := s.peekCodeWord(*pc)
	*pc += 2
	return value
}

func (s *State) nextLong(pc *uint32) uint32 {
	return uint32(s.nextWord(pc))<<16 | uint32(s.nextWord(pc))
}
