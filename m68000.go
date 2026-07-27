package iz68000

// NewM68000 returns an initialized M68000. Call Reset() to load the stack
// pointer and the program counter from the vector table.
func NewM68000(m Memory) *State {
	var s State
	s.mem = m
	s.opcodes = opcodes68000
	return &s
}

var opcodes68000 = buildOpcodeTable(opcodeDefs68000())

/*
The opcode families of the 68000. See the "Operation Code Map" appendix of the
manual for the encodings and the "Instruction Execution Times" one for the
cycles.

The cycles are the base time of the instruction, the effective address
calculation time is added when expanding the pattern. They are not exact yet
for the operands on address registers and for some of the long variants, the
Harte suite will tell.
*/
func opcodeDefs68000() []opcodeDef {
	defs := []opcodeDef{
		/*
			Data movement
		*/
		{name: "MOVE", pattern: "00zznnnMMMmmmrrr", ea: eaAll, ea2: eaDataAlterable,
			timing: timingMove, operands: operandsMove, cycles: 4, build: buildOpMove},
		{name: "MOVEA", pattern: "0011nnn001mmmrrr", size: sizeWord, ea: eaAll,
			timing: timingEA, operands: operandsEAToAddressReg, cycles: 4, build: buildOpMoveA},
		{name: "MOVEA", pattern: "0010nnn001mmmrrr", size: sizeLong, ea: eaAll,
			timing: timingEA, operands: operandsEAToAddressReg, cycles: 4, build: buildOpMoveA},
		{name: "MOVEQ", pattern: "0111nnn0xxxxxxxx", size: sizeLong,
			operands: operandsMoveQuick, cycles: 4, action: opMOVEQ},
		{name: "LEA", pattern: "0100nnn111mmmrrr", size: sizeLong, ea: eaControl,
			timing: timingEA, operands: operandsEAToAddressReg, cycles: 4, action: opLEA},
		{name: "PEA", pattern: "0100100001mmmrrr", size: sizeLong, ea: eaControl,
			timing: timingEA, operands: operandsEA, cycles: 12, action: opPEA},
		{name: "EXT", pattern: "0100100010000nnn", size: sizeWord,
			operands: operandsDataReg, cycles: 4, build: buildOpExt},
		{name: "EXT", pattern: "0100100011000nnn", size: sizeLong,
			operands: operandsDataReg, cycles: 4, build: buildOpExt},
		{name: "SWAP", pattern: "0100100001000nnn", size: sizeWord,
			operands: operandsDataReg, cycles: 4, action: opSWAP},

		/*
			Immediate to effective address
		*/
		{name: "ORI", pattern: "00000000ssmmmrrr", ea: eaDataAlterable, timing: timingEA,
			operands: operandsImmediateToEA, cycles: 8, cyclesL: 16, rmw: true,
			build: buildAluImmediate(aluOr, true)},
		{name: "ANDI", pattern: "00000010ssmmmrrr", ea: eaDataAlterable, timing: timingEA,
			operands: operandsImmediateToEA, cycles: 8, cyclesL: 16, rmw: true,
			build: buildAluImmediate(aluAnd, true)},
		{name: "SUBI", pattern: "00000100ssmmmrrr", ea: eaDataAlterable, timing: timingEA,
			operands: operandsImmediateToEA, cycles: 8, cyclesL: 16, rmw: true,
			build: buildAluImmediate(aluSub, true)},
		{name: "ADDI", pattern: "00000110ssmmmrrr", ea: eaDataAlterable, timing: timingEA,
			operands: operandsImmediateToEA, cycles: 8, cyclesL: 16, rmw: true,
			build: buildAluImmediate(aluAdd, true)},
		{name: "EORI", pattern: "00001010ssmmmrrr", ea: eaDataAlterable, timing: timingEA,
			operands: operandsImmediateToEA, cycles: 8, cyclesL: 16, rmw: true,
			build: buildAluImmediate(aluEor, true)},
		{name: "CMPI", pattern: "00001100ssmmmrrr", ea: eaDataAlterable, timing: timingEA,
			operands: operandsImmediateToEA, cycles: 8, cyclesL: 12,
			build: buildAluImmediate(aluCmp, false)},

		/*
			Quick immediate. The data of 1 to 8 is encoded on the opcode.
		*/
		{name: "ADDQ", pattern: "0101nnn0ssmmmrrr", ea: eaAlterable, timing: timingEA,
			operands: operandsQuickToEA, cycles: 4, cyclesL: 8, rmw: true,
			build: buildQuick(aluAdd)},
		{name: "SUBQ", pattern: "0101nnn1ssmmmrrr", ea: eaAlterable, timing: timingEA,
			operands: operandsQuickToEA, cycles: 4, cyclesL: 8, rmw: true,
			build: buildQuick(aluSub)},

		/*
			Arithmetic and logic with a data register. The bit 8 selects the
			direction, the bits 7-6 the size.
		*/
		{name: "OR", pattern: "1000nnn0ssmmmrrr", ea: eaData, timing: timingEA,
			operands: operandsEAToDataReg, cycles: 4, cyclesL: 6,
			build: buildAluToRegister(aluOr, true)},
		{name: "OR", pattern: "1000nnn1ssmmmrrr", ea: eaMemoryAlterable, timing: timingEA,
			operands: operandsDataRegToEA, cycles: 8, cyclesL: 12,
			build: buildAluToMemory(aluOr)},
		{name: "AND", pattern: "1100nnn0ssmmmrrr", ea: eaData, timing: timingEA,
			operands: operandsEAToDataReg, cycles: 4, cyclesL: 6,
			build: buildAluToRegister(aluAnd, true)},
		{name: "AND", pattern: "1100nnn1ssmmmrrr", ea: eaMemoryAlterable, timing: timingEA,
			operands: operandsDataRegToEA, cycles: 8, cyclesL: 12,
			build: buildAluToMemory(aluAnd)},
		{name: "SUB", pattern: "1001nnn0ssmmmrrr", ea: eaAll, timing: timingEA,
			operands: operandsEAToDataReg, cycles: 4, cyclesL: 6,
			build: buildAluToRegister(aluSub, true)},
		{name: "SUB", pattern: "1001nnn1ssmmmrrr", ea: eaMemoryAlterable, timing: timingEA,
			operands: operandsDataRegToEA, cycles: 8, cyclesL: 12,
			build: buildAluToMemory(aluSub)},
		{name: "ADD", pattern: "1101nnn0ssmmmrrr", ea: eaAll, timing: timingEA,
			operands: operandsEAToDataReg, cycles: 4, cyclesL: 6,
			build: buildAluToRegister(aluAdd, true)},
		{name: "ADD", pattern: "1101nnn1ssmmmrrr", ea: eaMemoryAlterable, timing: timingEA,
			operands: operandsDataRegToEA, cycles: 8, cyclesL: 12,
			build: buildAluToMemory(aluAdd)},
		{name: "CMP", pattern: "1011nnn0ssmmmrrr", ea: eaAll, timing: timingEA,
			operands: operandsEAToDataReg, cycles: 4, cyclesL: 6,
			build: buildAluToRegister(aluCmp, false)},
		{name: "EOR", pattern: "1011nnn1ssmmmrrr", ea: eaDataAlterable, timing: timingEA,
			operands: operandsDataRegToEA, cycles: 4, cyclesL: 8, rmw: true,
			build: buildAluToMemory(aluEor)},

		/*
			Arithmetic with an address register. The source is sign extended
			to a long and only CMPA changes the flags.
		*/
		{name: "SUBA", pattern: "1001nnn011mmmrrr", size: sizeWord, ea: eaAll, timing: timingEA,
			operands: operandsEAToAddressReg, cycles: 8, build: buildAluAddress(aluSub, true)},
		{name: "SUBA", pattern: "1001nnn111mmmrrr", size: sizeLong, ea: eaAll, timing: timingEA,
			operands: operandsEAToAddressReg, cycles: 6, build: buildAluAddress(aluSub, true)},
		{name: "ADDA", pattern: "1101nnn011mmmrrr", size: sizeWord, ea: eaAll, timing: timingEA,
			operands: operandsEAToAddressReg, cycles: 8, build: buildAluAddress(aluAdd, true)},
		{name: "ADDA", pattern: "1101nnn111mmmrrr", size: sizeLong, ea: eaAll, timing: timingEA,
			operands: operandsEAToAddressReg, cycles: 6, build: buildAluAddress(aluAdd, true)},
		{name: "CMPA", pattern: "1011nnn011mmmrrr", size: sizeWord, ea: eaAll, timing: timingEA,
			operands: operandsEAToAddressReg, cycles: 6, build: buildAluAddress(aluCmp, false)},
		{name: "CMPA", pattern: "1011nnn111mmmrrr", size: sizeLong, ea: eaAll, timing: timingEA,
			operands: operandsEAToAddressReg, cycles: 6, build: buildAluAddress(aluCmp, false)},

		/*
			Single operand
		*/
		{name: "NEGX", pattern: "01000000ssmmmrrr", ea: eaDataAlterable, timing: timingEA,
			operands: operandsEA, cycles: 4, cyclesL: 6, rmw: true, build: buildOpNEGX},
		{name: "CLR", pattern: "01000010ssmmmrrr", ea: eaDataAlterable, timing: timingEA,
			operands: operandsEA, cycles: 4, cyclesL: 6, rmw: true, build: buildOpCLR},
		{name: "NEG", pattern: "01000100ssmmmrrr", ea: eaDataAlterable, timing: timingEA,
			operands: operandsEA, cycles: 4, cyclesL: 6, rmw: true, build: buildOpNEG},
		{name: "NOT", pattern: "01000110ssmmmrrr", ea: eaDataAlterable, timing: timingEA,
			operands: operandsEA, cycles: 4, cyclesL: 6, rmw: true, build: buildOpNOT},
		{name: "TST", pattern: "01001010ssmmmrrr", ea: eaDataAlterable, timing: timingEA,
			operands: operandsEA, cycles: 4, build: buildOpTST},

		/*
			Bit manipulation. The bit number comes from a data register or,
			for the static variants, from the word after the opcode.
		*/
		{name: "BTST", pattern: "0000nnn100mmmrrr", sizeFromMode: true, ea: eaData, timing: timingEA,
			operands: operandsBitToEA, cycles: 6, action: buildOpBit(bitTest, false)},
		{name: "BCHG", pattern: "0000nnn101mmmrrr", sizeFromMode: true, ea: eaDataAlterable, timing: timingEA,
			operands: operandsBitToEA, cycles: 8, action: buildOpBit(bitChange, false)},
		{name: "BCLR", pattern: "0000nnn110mmmrrr", sizeFromMode: true, ea: eaDataAlterable, timing: timingEA,
			operands: operandsBitToEA, cycles: 10, action: buildOpBit(bitClear, false)},
		{name: "BSET", pattern: "0000nnn111mmmrrr", sizeFromMode: true, ea: eaDataAlterable, timing: timingEA,
			operands: operandsBitToEA, cycles: 8, action: buildOpBit(bitSet, false)},
		{name: "BTST", pattern: "0000100000mmmrrr", sizeFromMode: true, ea: eaData, timing: timingEA,
			operands: operandsBitStaticToEA, cycles: 10, action: buildOpBit(bitTest, true)},
		{name: "BCHG", pattern: "0000100001mmmrrr", sizeFromMode: true, ea: eaDataAlterable, timing: timingEA,
			operands: operandsBitStaticToEA, cycles: 12, action: buildOpBit(bitChange, true)},
		{name: "BCLR", pattern: "0000100010mmmrrr", sizeFromMode: true, ea: eaDataAlterable, timing: timingEA,
			operands: operandsBitStaticToEA, cycles: 14, action: buildOpBit(bitClear, true)},
		{name: "BSET", pattern: "0000100011mmmrrr", sizeFromMode: true, ea: eaDataAlterable, timing: timingEA,
			operands: operandsBitStaticToEA, cycles: 12, action: buildOpBit(bitSet, true)},

		/*
			Program control
		*/
		{name: "BRA", pattern: "01100000xxxxxxxx", operands: operandsBranch,
			cycles: 10, action: buildOpBranch(condT)},
		{name: "BSR", pattern: "01100001xxxxxxxx", operands: operandsBranch,
			cycles: 18, action: opBSR},
		{name: "JMP", pattern: "0100111011mmmrrr", size: sizeLong, ea: eaControl,
			timing: timingEA, operands: operandsEA, cycles: 4, action: opJMP},
		{name: "JSR", pattern: "0100111010mmmrrr", size: sizeLong, ea: eaControl,
			timing: timingEA, operands: operandsEA, cycles: 12, action: opJSR},
		{name: "RTS", pattern: "0100111001110101", operands: operandsNone,
			cycles: 16, action: opRTS},
		{name: "RTR", pattern: "0100111001110111", operands: operandsNone,
			cycles: 20, action: opRTR},
		{name: "RTE", pattern: "0100111001110011", operands: operandsNone,
			cycles: 20, action: opRTE},
		{name: "LINK", pattern: "0100111001010nnn", operands: operandsLink,
			cycles: 16, action: opLINK},
		{name: "UNLK", pattern: "0100111001011nnn", operands: operandsAddressReg,
			cycles: 12, action: opUNLK},
		{name: "TRAP", pattern: "010011100100xxxx", operands: operandsTrap,
			cycles: 4, action: opTRAP},
		{name: "TRAPV", pattern: "0100111001110110", operands: operandsNone,
			cycles: 4, action: opTRAPV},
		{name: "NOP", pattern: "0100111001110001", operands: operandsNone,
			cycles: 4, action: opNOP},
		{name: "RESET", pattern: "0100111001110000", operands: operandsNone,
			cycles: 132, action: opRESET},
		{name: "STOP", pattern: "0100111001110010", operands: operandsStop,
			cycles: 4, action: opSTOP},
		{name: "ILLEGAL", pattern: "0100101011111100", operands: operandsNone,
			cycles: 4, action: opILLEGAL},

		/*
			The unimplemented instruction ranges. The Macintosh uses the line
			A traps for all the toolbox and operating system calls.
		*/
		{name: "LINEA", pattern: "1010xxxxxxxxxxxx", operands: operandsNone,
			cycles: 4, action: opLineA},
		{name: "LINEF", pattern: "1111xxxxxxxxxxxx", operands: operandsNone,
			cycles: 4, action: opLineF},
	}

	defs = append(defs, shiftDefs()...)

	// Bcc, with the conditions 0 and 1 taken by BRA and BSR
	defs = append(defs, conditionalDefs(opcodeDef{
		pattern: "0110ccccxxxxxxxx", operands: operandsBranch, cycles: 8,
	}, "B", buildOpBranch, condT, condF)...)

	// Scc sets a byte to all ones or all zeros, DBcc is a loop primitive
	defs = append(defs, conditionalDefs(opcodeDef{
		pattern: "0101cccc11mmmrrr", size: sizeByte, ea: eaDataAlterable,
		timing: timingEA, operands: operandsEA, cycles: 4, rmw: true,
	}, "S", buildOpScc)...)
	defs = append(defs, conditionalDefs(opcodeDef{
		pattern: "0101cccc11001nnn", size: sizeWord, operands: operandsDbcc, cycles: 10,
	}, "DB", buildOpDBcc)...)

	return defs
}

// shiftDefs builds the 8 shift and rotate instructions, each one with a
// variant that shifts a data register and another that shifts a word in
// memory by a single bit
func shiftDefs() []opcodeDef {
	kinds := []struct {
		name string
		kind int
		bits string
	}{
		{"AS", shiftArithmetic, "00"},
		{"LS", shiftLogical, "01"},
		{"ROX", shiftRotateExtend, "10"},
		{"RO", shiftRotate, "11"},
	}

	defs := make([]opcodeDef, 0, 16)
	for _, k := range kinds {
		for _, d := range []struct {
			suffix string
			left   bool
			bit    string
		}{{"R", false, "0"}, {"L", true, "1"}} {
			defs = append(defs,
				opcodeDef{
					name:     k.name + d.suffix,
					pattern:  "1110nnn" + d.bit + "ssx" + k.bits + "nnn",
					operands: operandsShift, cycles: 6, cyclesL: 8,
					build: buildShiftRegister(k.kind, d.left),
				},
				opcodeDef{
					name:    k.name + d.suffix,
					pattern: "11100" + k.bits + d.bit + "11mmmrrr",
					size:    sizeWord, ea: eaMemoryAlterable, timing: timingEA,
					operands: operandsEA, cycles: 8,
					action: buildOpShiftMemory(k.kind, d.left),
				})
		}
	}
	return defs
}
