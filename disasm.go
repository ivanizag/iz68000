package iz68000

import "fmt"

// The shapes of the operands, used only to disassemble. They tell how to read
// the extension words that follow the opcode.
const (
	operandsNone           = iota
	operandsEA             // <ea>
	operandsEAToDataReg    // <ea>,Dn
	operandsDataRegToEA    // Dn,<ea>
	operandsEAToAddressReg // <ea>,An
	operandsImmediateToEA  // #<data>,<ea>
	operandsQuickToEA      // #<1-8>,<ea>
	operandsMove           // <ea>,<ea>
	operandsMoveQuick      // #<data>,Dn
	operandsBranch         // <label>
	operandsDbcc           // Dn,<label>
	operandsShift          // #<count>,Dn or Dm,Dn
	operandsDataReg        // Dn
	operandsAddressReg     // An
	operandsBitToEA        // Dn,<ea>
	operandsBitStaticToEA  // #<bit>,<ea>
	operandsLink           // An,#<displacement>
	operandsTrap           // #<vector>
	operandsStop           // #<data>
	operandsMovemToMemory  // <register list>,<ea>
	operandsMovemToReg     // <ea>,<register list>
	operandsMovepToMemory  // Dn,(d16,An)
	operandsMovepToReg     // (d16,An),Dn
	operandsExtended       // Dy,Dx or -(Ay),-(Ax)
	operandsCmpm           // (Ay)+,(Ax)+
	operandsExg            // Rx,Ry
	operandsImmediateToCCR // #<data>,CCR
	operandsImmediateToSR  // #<data>,SR
	operandsEAToCCR        // <ea>,CCR
	operandsEAToSR         // <ea>,SR
	operandsSRToEA         // SR,<ea>
	operandsRegToUSP       // An,USP
	operandsUSPToReg       // USP,An
)

// operandsString disassembles an instruction, advancing pc over the extension
// words it uses
func (s *State) operandsString(ir uint16, op *opcode, pc *uint32) string {
	mnemonic := op.name
	if op.size != 0 {
		mnemonic += "." + sizeName(op.size)
	}

	var operands string
	switch op.operands {
	case operandsNone:

	case operandsEA:
		operands = s.operandString(eaMode(ir), eaReg(ir), op.size, pc)

	case operandsEAToDataReg:
		operands = fmt.Sprintf("%s,D%d",
			s.operandString(eaMode(ir), eaReg(ir), op.size, pc), opReg(ir))

	case operandsDataRegToEA:
		operands = fmt.Sprintf("D%d,%s",
			opReg(ir), s.operandString(eaMode(ir), eaReg(ir), op.size, pc))

	case operandsEAToAddressReg:
		operands = fmt.Sprintf("%s,A%d",
			s.operandString(eaMode(ir), eaReg(ir), op.size, pc), opReg(ir))

	case operandsImmediateToEA:
		// The immediate comes before the extension words of the destination
		immediate := s.operandString(modeExtended, modeExtImmediate, op.size, pc)
		operands = fmt.Sprintf("%s,%s",
			immediate, s.operandString(eaMode(ir), eaReg(ir), op.size, pc))

	case operandsQuickToEA:
		data := opReg(ir)
		if data == 0 {
			data = 8
		}
		operands = fmt.Sprintf("#%d,%s",
			data, s.operandString(eaMode(ir), eaReg(ir), op.size, pc))

	case operandsMove:
		source := s.operandString(eaMode(ir), eaReg(ir), op.size, pc)
		operands = fmt.Sprintf("%s,%s",
			source, s.operandString(dstMode(ir), dstReg(ir), op.size, pc))

	case operandsMoveQuick:
		operands = fmt.Sprintf("#$%x,D%d", uint8(ir), opReg(ir))

	case operandsBranch:
		operands = fmt.Sprintf("$%x", s.branchTarget(ir, pc))

	case operandsDbcc:
		base := *pc
		operands = fmt.Sprintf("D%d,$%x", eaReg(ir),
			base+signExtend(uint32(s.nextWord(pc)), sizeWord))

	case operandsShift:
		if ir&0x0020 == 0 {
			count := opReg(ir)
			if count == 0 {
				count = 8
			}
			operands = fmt.Sprintf("#%d,D%d", count, eaReg(ir))
		} else {
			operands = fmt.Sprintf("D%d,D%d", opReg(ir), eaReg(ir))
		}

	case operandsDataReg:
		operands = fmt.Sprintf("D%d", eaReg(ir))

	case operandsAddressReg:
		operands = fmt.Sprintf("A%d", eaReg(ir))

	case operandsBitToEA:
		operands = fmt.Sprintf("D%d,%s",
			opReg(ir), s.operandString(eaMode(ir), eaReg(ir), sizeByte, pc))

	case operandsBitStaticToEA:
		bit := s.nextWord(pc)
		operands = fmt.Sprintf("#%d,%s",
			bit, s.operandString(eaMode(ir), eaReg(ir), sizeByte, pc))

	case operandsLink:
		operands = fmt.Sprintf("A%d,#$%x", eaReg(ir), s.nextWord(pc))

	case operandsTrap:
		operands = fmt.Sprintf("#%d", ir&0xf)

	case operandsStop:
		operands = fmt.Sprintf("#$%04x", s.nextWord(pc))

	case operandsMovemToMemory:
		list := registerListString(s.nextWord(pc), eaMode(ir) == modeIndirectPredecrement)
		operands = fmt.Sprintf("%s,%s",
			list, s.operandString(eaMode(ir), eaReg(ir), op.size, pc))

	case operandsMovemToReg:
		list := registerListString(s.nextWord(pc), false)
		operands = fmt.Sprintf("%s,%s",
			s.operandString(eaMode(ir), eaReg(ir), op.size, pc), list)

	case operandsMovepToMemory:
		operands = fmt.Sprintf("D%d,($%x,A%d)", opReg(ir), s.nextWord(pc), eaReg(ir))

	case operandsMovepToReg:
		operands = fmt.Sprintf("($%x,A%d),D%d", s.nextWord(pc), eaReg(ir), opReg(ir))

	case operandsExtended:
		if ir&0x0008 == 0 {
			operands = fmt.Sprintf("D%d,D%d", eaReg(ir), opReg(ir))
		} else {
			operands = fmt.Sprintf("-(A%d),-(A%d)", eaReg(ir), opReg(ir))
		}

	case operandsCmpm:
		operands = fmt.Sprintf("(A%d)+,(A%d)+", eaReg(ir), opReg(ir))

	case operandsExg:
		operands = fmt.Sprintf("%s,%s", exgRegisterName(ir, true), exgRegisterName(ir, false))

	case operandsImmediateToCCR:
		operands = fmt.Sprintf("#$%x,CCR", s.nextWord(pc))

	case operandsImmediateToSR:
		operands = fmt.Sprintf("#$%x,SR", s.nextWord(pc))

	case operandsEAToCCR:
		operands = s.operandString(eaMode(ir), eaReg(ir), sizeWord, pc) + ",CCR"

	case operandsEAToSR:
		operands = s.operandString(eaMode(ir), eaReg(ir), sizeWord, pc) + ",SR"

	case operandsSRToEA:
		operands = "SR," + s.operandString(eaMode(ir), eaReg(ir), sizeWord, pc)

	case operandsRegToUSP:
		operands = fmt.Sprintf("A%d,USP", eaReg(ir))

	case operandsUSPToReg:
		operands = fmt.Sprintf("USP,A%d", eaReg(ir))

	default:
		operands = "UNKNOWN OPERANDS"
	}

	if operands == "" {
		return mnemonic
	}
	return fmt.Sprintf("%-8s %s", mnemonic, operands)
}

// registerListString expands the mask of MOVEM, collapsing the runs of
// consecutive registers into ranges. The predecrement mode reverses the order
// of the bits.
func registerListString(mask uint16, reversed bool) string {
	names := ""
	for i := 0; i < regCount; {
		bit := i
		if reversed {
			bit = regCount - 1 - i
		}
		if mask&(1<<bit) == 0 {
			i++
			continue
		}

		// Find how far the run of consecutive registers goes, without
		// crossing from the data to the address registers
		last := i
		for last+1 < regCount && last+1 != regA0 {
			next := last + 1
			if reversed {
				next = regCount - 1 - (last + 1)
			}
			if mask&(1<<next) == 0 {
				break
			}
			last++
		}

		if names != "" {
			names += "/"
		}
		names += registerName(i)
		if last > i {
			names += "-" + registerName(last)
		}
		i = last + 1
	}

	if names == "" {
		return "#0"
	}
	return names
}

func registerName(i int) string {
	if i < regA0 {
		return fmt.Sprintf("D%d", i)
	}
	return fmt.Sprintf("A%d", i-regA0)
}

// The opmode field of EXG, that tells which of the two registers are data
// registers and which are address ones
const (
	exgDataData       = 0x28 // 101000
	exgAddressAddress = 0x29 // 101001
	exgDataAddress    = 0x31 // 110001
)

func exgRegisterName(ir uint16, first bool) string {
	opmode := ir >> 3 & 0x3f
	if first {
		if opmode == exgAddressAddress {
			return fmt.Sprintf("A%d", opReg(ir))
		}
		return fmt.Sprintf("D%d", opReg(ir))
	}
	if opmode == exgDataData {
		return fmt.Sprintf("D%d", eaReg(ir))
	}
	return fmt.Sprintf("A%d", eaReg(ir))
}

// branchTarget resolves the displacement of Bcc, BRA and BSR. A zero byte
// displacement means that a word displacement follows the opcode.
func (s *State) branchTarget(ir uint16, pc *uint32) uint32 {
	base := *pc
	if uint8(ir) == 0 {
		return base + signExtend(uint32(s.nextWord(pc)), sizeWord)
	}
	return base + signExtend(uint32(ir), sizeByte)
}
