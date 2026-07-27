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

	default:
		operands = "UNKNOWN OPERANDS"
	}

	if operands == "" {
		return mnemonic
	}
	return fmt.Sprintf("%-8s %s", mnemonic, operands)
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
