package iz68000

import "math/bits"

/*
The instructions that don't fit the regular effective address patterns:
multiple register transfers, multiply and divide, decimal arithmetic, the
multiprecision operations and the ones that reach the status register.
*/

/*
MOVEM transfers a list of registers, given by a mask on the word after the
opcode. The bit 0 of the mask is D0 and the bit 15 is A7, except on the
predecrement mode where the order is reversed.
*/

func buildOpMOVEMToMemory(size int) opFunc {
	return func(s *State, ir uint16, op *opcode) {
		mask := s.fetchWord()
		mode, reg := eaMode(ir), eaReg(ir)

		if mode == modeIndirectPredecrement {
			// The registers go from A7 down to D0, decrementing before each
			// one. An address register on the list is stored with the value
			// it had before the transfer started.
			address := s.reg.getA(reg)
			for i := 0; i < regCount; i++ {
				if mask&(1<<i) == 0 {
					continue
				}
				address -= uint32(size)
				s.pokeSizedReversed(address, size, s.reg.getRegister(regCount-1-i))
				s.extraCycles += 2 * size
			}
			s.reg.setA(reg, address)
			return
		}

		operand := s.resolveOperand(mode, reg, size)
		address := operand.address
		for i := 0; i < regCount; i++ {
			if mask&(1<<i) == 0 {
				continue
			}
			s.pokeSized(address, size, s.reg.getRegister(i))
			address += uint32(size)
			s.extraCycles += 2 * size
		}
	}
}

func buildOpMOVEMToRegister(size int) opFunc {
	return func(s *State, ir uint16, op *opcode) {
		mask := s.fetchWord()
		mode, reg := eaMode(ir), eaReg(ir)

		address := s.reg.getA(reg)
		if mode != modeIndirectPostincrement {
			operand := s.resolveOperand(mode, reg, size)
			address = operand.address
		}

		for i := 0; i < regCount; i++ {
			if mask&(1<<i) == 0 {
				continue
			}
			// The words are sign extended, the registers are always fully
			// written even on the word variant
			s.reg.setRegister(i, signExtend(s.peekSized(address, size), size))
			address += uint32(size)
			s.extraCycles += 2 * size
		}

		if mode == modeIndirectPostincrement {
			// Done last, it wins if the address register was on the list
			s.reg.setA(reg, address)
		}
	}
}

/*
MOVEP moves the bytes of a register to alternate bytes of memory, to reach the
8 bit peripherals wired to only half of the data bus.
*/

func buildOpMOVEP(size int, toMemory bool) opFunc {
	return func(s *State, ir uint16, op *opcode) {
		displacement := signExtend(uint32(s.fetchWord()), sizeWord)
		address := s.reg.getA(eaReg(ir)) + displacement
		reg := opReg(ir)

		if toMemory {
			value := s.reg.getD(reg)
			for shift := (size - 1) * 8; shift >= 0; shift -= 8 {
				s.pokeByte(address, value>>uint(shift))
				address += 2
			}
			return
		}

		var value uint32
		for i := 0; i < size; i++ {
			value = value<<8 | s.peekByte(address)
			address += 2
		}
		s.reg.setDSized(reg, size, value)
	}
}

/*
Multiply and divide, always 16 bit operands on the 68000
*/

func buildOpMultiply(signed bool) opFunc {
	return func(s *State, ir uint16, op *opcode) {
		src := s.resolveOperand(eaMode(ir), eaReg(ir), sizeWord)
		value := uint16(s.readOperand(&src, sizeWord))

		reg := opReg(ir)
		multiplicand := uint16(s.reg.getD(reg))

		var result uint32
		if signed {
			result = uint32(int32(int16(multiplicand)) * int32(int16(value)))
			s.extraCycles = multiplySignedCycles(value)
		} else {
			result = uint32(multiplicand) * uint32(value)
			s.extraCycles = 2 * bits.OnesCount16(value)
		}

		s.reg.setDSized(reg, sizeLong, result)
		s.reg.updateFlagsLogic(result, sizeLong)
	}
}

// multiplySignedCycles counts the pairs of different consecutive bits of the
// source, the microcode of MULS iterates on them
func multiplySignedCycles(value uint16) int {
	work := uint32(value) << 1
	count := 0
	for i := 0; i < 16; i++ {
		if pair := work >> i & 3; pair == 1 || pair == 2 {
			count++
		}
	}
	return 2 * count
}

func buildOpDivide(signed bool) opFunc {
	return func(s *State, ir uint16, op *opcode) {
		src := s.resolveOperand(eaMode(ir), eaReg(ir), sizeWord)
		divisor := uint16(s.readOperand(&src, sizeWord))
		reg := opReg(ir)

		if divisor == 0 {
			s.reg.clearFlag(flagC)
			s.raiseException(vectorZeroDivide)
		}

		var quotient, remainder uint32
		if signed {
			dividend := int32(s.reg.getD(reg))
			signedDivisor := int32(int16(divisor))

			// The only division that overflows the 32 bit quotient, and the
			// one that would panic in Go
			if dividend == -1<<31 && signedDivisor == -1 {
				s.divideOverflow()
				return
			}

			signedQuotient := dividend / signedDivisor
			if signedQuotient > 0x7fff || signedQuotient < -0x8000 {
				s.divideOverflow()
				return
			}
			quotient = uint32(signedQuotient) & 0xffff
			remainder = uint32(dividend%signedDivisor) & 0xffff
		} else {
			dividend := s.reg.getD(reg)
			if dividend/uint32(divisor) > 0xffff {
				s.divideOverflow()
				return
			}
			quotient = dividend / uint32(divisor)
			remainder = dividend % uint32(divisor)
		}

		// The remainder goes on the high word and the quotient on the low one
		s.reg.setDSized(reg, sizeLong, remainder<<16|quotient)
		s.reg.updateFlagsLogic(quotient, sizeWord)
	}
}

// divideOverflow aborts a division whose quotient does not fit on a word. The
// destination is left untouched and N, documented as undefined, is set.
func (s *State) divideOverflow() {
	s.reg.setFlag(flagV | flagN)
	s.reg.clearFlag(flagC | flagZ)
}

/*
Decimal arithmetic. The 68000 leaves N and V undefined, the values here are
the ones the hardware happens to produce.
*/

// bcdOperands returns the two byte operands of ABCD and SBCD and, when they
// are in memory, the destination to write the result to
func (s *State) bcdOperands(ir uint16) (uint32, uint32, operand) {
	if ir&0x0008 == 0 {
		return s.reg.getDSized(eaReg(ir), sizeByte),
			s.reg.getDSized(opReg(ir), sizeByte),
			operand{mode: modeDataRegister, reg: opReg(ir)}
	}

	// The predecrement variant, the source is resolved first
	srcOperand := s.resolveOperand(modeIndirectPredecrement, eaReg(ir), sizeByte)
	src := s.readOperand(&srcOperand, sizeByte)
	dstOperand := s.resolveOperand(modeIndirectPredecrement, opReg(ir), sizeByte)
	return src, s.readOperand(&dstOperand, sizeByte), dstOperand
}

func opABCD(s *State, ir uint16, op *opcode) {
	src, dst, dstOperand := s.bcdOperands(ir)

	result := src&0x0f + dst&0x0f + s.reg.getFlagBit(flagX)
	overflow := ^result
	if result > 9 {
		result += 6
	}
	result += src&0xf0 + dst&0xf0

	// The digits above 9 can push the corrected low nibble up to $f without
	// carrying, so the threshold is the whole decimal hundred
	carry := result >= 0xa0
	if carry {
		result -= 0xa0
	}
	s.reg.updateFlag(flagC, carry)
	s.reg.updateFlag(flagX, carry)

	s.finishBcd(result, overflow, &dstOperand)
}

func opSBCD(s *State, ir uint16, op *opcode) {
	src, dst, dstOperand := s.bcdOperands(ir)

	result := dst&0x0f - src&0x0f - s.reg.getFlagBit(flagX)
	overflow := ^result
	if result > 0x0f {
		// The subtraction of the low digits went negative and wrapped, the
		// operands can have digits above 9 and those don't borrow
		result -= 6
	}
	result += dst&0xf0 - src&0xf0

	borrow := result > 0xff
	if borrow {
		result += 0xa0
	}
	s.reg.updateFlag(flagC, borrow)
	s.reg.updateFlag(flagX, borrow)

	s.finishBcd(result, overflow, &dstOperand)
}

func opNBCD(s *State, ir uint16, op *opcode) {
	dstOperand := s.resolveOperand(eaMode(ir), eaReg(ir), sizeByte)
	dst := s.readOperand(&dstOperand, sizeByte)

	result := 0 - dst&0x0f - s.reg.getFlagBit(flagX)
	overflow := ^result
	if result > 0x0f {
		result -= 6
	}
	result -= dst & 0xf0

	borrow := result > 0xff
	if borrow {
		result += 0xa0
	}
	s.reg.updateFlag(flagC, borrow)
	s.reg.updateFlag(flagX, borrow)

	s.finishBcd(result, overflow, &dstOperand)
}

// finishBcd writes the result and updates the flags shared by the three
// decimal instructions. Z is only cleared, so that it reflects a whole
// multiprecision operation.
func (s *State) finishBcd(result uint32, overflow uint32, dst *operand) {
	result &= 0xff
	s.reg.updateFlag(flagV, overflow&result&0x80 != 0)
	s.reg.updateFlag(flagN, result&0x80 != 0)
	if result != 0 {
		s.reg.clearFlag(flagZ)
	}
	s.writeOperand(dst, sizeByte, result)
}

/*
Multiprecision arithmetic. Like the decimal instructions, they take the
operands from two data registers or from two predecremented addresses.
*/

func buildOpExtended(add bool, size int) opFunc {
	return func(s *State, ir uint16, op *opcode) {
		zero := s.reg.getFlag(flagZ)
		extend := s.reg.getFlagBit(flagX)

		var src, dst uint32
		var dstOperand operand
		switch {
		case ir&0x0008 == 0:
			src = s.reg.getDSized(eaReg(ir), size)
			dst = s.reg.getDSized(opReg(ir), size)
			dstOperand = operand{mode: modeDataRegister, reg: opReg(ir)}

		case size == sizeLong:
			// The two address registers are decremented before the transfer
			// but only written back once both longs have been read, so an
			// address error on either of them leaves them untouched
			srcAddress := s.reg.getA(eaReg(ir)) - sizeLong
			dstAddress := s.reg.getA(opReg(ir)) - sizeLong
			if eaReg(ir) == opReg(ir) {
				// The same register is decremented twice
				dstAddress = srcAddress - sizeLong
			}
			src = s.peekSizedReversed(srcAddress, sizeLong)
			dst = s.peekSizedReversed(dstAddress, sizeLong)
			s.reg.setA(eaReg(ir), srcAddress)
			s.reg.setA(opReg(ir), dstAddress)
			dstOperand = operand{mode: modeIndirect, reg: opReg(ir), address: dstAddress}

		default:
			srcOperand := s.resolveOperand(modeIndirectPredecrement, eaReg(ir), size)
			src = s.readOperand(&srcOperand, size)
			dstOperand = s.resolveOperand(modeIndirectPredecrement, opReg(ir), size)
			dst = s.readOperand(&dstOperand, size)
		}

		var result uint32
		if add {
			result = s.doAdd(dst, src, extend, size)
		} else {
			result = s.doSub(dst, src, extend, size)
		}

		// Z is only cleared, never set
		s.reg.updateFlag(flagZ, zero && result&maskOf(size) == 0)
		s.writeOperand(&dstOperand, size, result)
	}
}

func buildOpCMPM(size int) opFunc {
	return func(s *State, ir uint16, op *opcode) {
		srcOperand := s.resolveOperand(modeIndirectPostincrement, eaReg(ir), size)
		src := s.readOperand(&srcOperand, size)

		// The second address register is written back only once its own read
		// succeeds, unlike the usual rule for the word transfers
		dstAddress := s.reg.getA(opReg(ir))
		dst := s.peekSized(dstAddress, size)
		s.reg.setA(opReg(ir), dstAddress+stackAdjust(opReg(ir), size))

		s.doCompare(dst, src, 0, size)
	}
}

/*
Miscellaneous
*/

// opCHK traps if a data register is out of the range from zero to the operand
func opCHK(s *State, ir uint16, op *opcode) {
	src := s.resolveOperand(eaMode(ir), eaReg(ir), sizeWord)
	bound := int32(int16(s.readOperand(&src, sizeWord)))
	value := int32(int16(s.reg.getDSized(opReg(ir), sizeWord)))

	s.reg.updateFlag(flagZ, value == 0)
	s.reg.clearFlag(flagV | flagC)
	s.reg.updateFlag(flagN, value < 0)

	if value < 0 {
		s.raiseException(vectorChk)
	}
	if value > bound {
		// N is documented as undefined here, the hardware clears it
		s.reg.clearFlag(flagN)
		s.raiseException(vectorChk)
	}
}

// opTAS tests a byte and sets its most significant bit, with an indivisible
// read modify write cycle on the real bus
func opTAS(s *State, ir uint16, op *opcode) {
	dst := s.resolveOperand(eaMode(ir), eaReg(ir), sizeByte)
	value := s.readOperand(&dst, sizeByte)
	s.reg.updateFlagsLogic(value, sizeByte)
	s.writeOperand(&dst, sizeByte, value|0x80)
}

// buildOpEXG exchanges two registers. The bases select if each one of them is
// a data or an address register.
func buildOpEXG(firstBase int, secondBase int) opFunc {
	return func(s *State, ir uint16, op *opcode) {
		first := firstBase + opReg(ir)
		second := secondBase + eaReg(ir)

		value := s.reg.getRegister(first)
		s.reg.setRegister(first, s.reg.getRegister(second))
		s.reg.setRegister(second, value)
	}
}

/*
Access to the status register. Note that MOVE from SR is not privileged on the
68000, it became so on the 68010.
*/

func opMOVEfromSR(s *State, ir uint16, op *opcode) {
	dst := s.resolveOperand(eaMode(ir), eaReg(ir), sizeWord)
	// The 68000 reads the destination before writing it
	s.readOperand(&dst, sizeWord)
	s.writeOperand(&dst, sizeWord, uint32(s.reg.getSR()))
}

func opMOVEtoCCR(s *State, ir uint16, op *opcode) {
	src := s.resolveOperand(eaMode(ir), eaReg(ir), sizeWord)
	s.reg.setCCR(uint8(s.readOperand(&src, sizeWord)))
}

func opMOVEtoSR(s *State, ir uint16, op *opcode) {
	s.requireSupervisor()
	src := s.resolveOperand(eaMode(ir), eaReg(ir), sizeWord)
	s.reg.setSR(uint16(s.readOperand(&src, sizeWord)))
}

func opMOVEtoUSP(s *State, ir uint16, op *opcode) {
	s.requireSupervisor()
	s.reg.setUSP(s.reg.getA(eaReg(ir)))
}

func opMOVEfromUSP(s *State, ir uint16, op *opcode) {
	s.requireSupervisor()
	s.reg.setA(eaReg(ir), s.reg.getUSP())
}

func operationOr(a uint32, b uint32) uint32  { return a | b }
func operationAnd(a uint32, b uint32) uint32 { return a & b }
func operationEor(a uint32, b uint32) uint32 { return a ^ b }

func buildOpImmediateToCCR(operation func(uint32, uint32) uint32) opFunc {
	return func(s *State, ir uint16, op *opcode) {
		value := uint32(s.fetchWord())
		s.reg.setCCR(uint8(operation(uint32(s.reg.getCCR()), value)))
	}
}

func buildOpImmediateToSR(operation func(uint32, uint32) uint32) opFunc {
	return func(s *State, ir uint16, op *opcode) {
		// The privilege is checked before consuming the immediate
		s.requireSupervisor()
		value := uint32(s.fetchWord())
		s.reg.setSR(uint16(operation(uint32(s.reg.getSR()), value)))
	}
}

/*
Adapters for the families with a size variant
*/

func buildMOVEP(toMemory bool) func(int) opFunc {
	return func(size int) opFunc { return buildOpMOVEP(size, toMemory) }
}

func buildExtended(add bool) func(int) opFunc {
	return func(size int) opFunc { return buildOpExtended(add, size) }
}
