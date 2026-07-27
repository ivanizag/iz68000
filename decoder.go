package iz68000

import (
	"fmt"
	"strings"
)

/*
The 68000 opcodes are 16 bit words with the operands encoded on the free bits,
so a table with an entry per opcode word is needed. Instead of writing the
65536 entries, each family of opcodes is described by a bit pattern that is
expanded on init. The size, the addressing mode and the cycles taken by the
effective address calculation are resolved there, the actions only have to
extract the register numbers.
*/

// The characters of the patterns. Everything else is fixed to 0 or 1.
const (
	patternSize     = 's' // Size field on the bits 7-6: 00 byte, 01 word, 10 long
	patternMoveSize = 'z' // Size field of MOVE on the bits 13-12: 01 byte, 11 word, 10 long
	patternEAMode   = 'm' // Effective address on the bits 5-0
	patternEA2Mode  = 'M' // Second effective address of MOVE on the bits 11-6
	patternAny      = 'x' // Don't care
)

// How the effective address calculation times are added to the base time
const (
	timingFixed = iota
	timingEA
	timingMove
)

// opcodeDef describes a family of opcodes sharing the same action
type opcodeDef struct {
	name    string
	pattern string

	size         int  // Used when the pattern has no size field
	sizeFromMode bool // Long on a data register, byte otherwise, as the bit operations

	ea       int // Addressing modes accepted on the bits 5-0
	ea2      int // Addressing modes accepted on the bits 11-6
	timing   int
	operands int

	cycles  int  // Base cycles for byte and word
	cyclesL int  // Base cycles for long, defaults to cycles
	rmw     bool // Add 4 cycles when the destination is in memory

	action opFunc           // For the instructions without variants
	build  func(int) opFunc // For the instructions with a size variant
}

func buildOpcodeTable(defs []opcodeDef) *[65536]opcode {
	var ops [65536]opcode
	for i := range defs {
		registerDef(&ops, &defs[i])
	}
	return &ops
}

func registerDef(ops *[65536]opcode, def *opcodeDef) {
	mask, value := patternBits(def.pattern)
	hasSize := strings.IndexByte(def.pattern, patternSize) != -1
	hasMoveSize := strings.IndexByte(def.pattern, patternMoveSize) != -1
	hasEA := strings.IndexByte(def.pattern, patternEAMode) != -1
	hasEA2 := strings.IndexByte(def.pattern, patternEA2Mode) != -1

	// The actions are shared by all the opcodes of the family with the same
	// size, there is no need to build a closure per opcode word
	var actions [sizeLong + 1]opFunc

	// Enumerate the values of the bits not fixed by the pattern
	free := ^mask
	for sub := free; ; sub = (sub - 1) & free {
		ir := value | sub
		registerOpcode(ops, def, ir, &actions, hasSize, hasMoveSize, hasEA, hasEA2)
		if sub == 0 {
			break
		}
	}
}

func registerOpcode(ops *[65536]opcode, def *opcodeDef, ir uint16,
	actions *[sizeLong + 1]opFunc, hasSize bool, hasMoveSize bool, hasEA bool, hasEA2 bool) {

	size := def.size
	switch {
	case hasSize:
		switch ir >> 6 & 3 {
		case 0:
			size = sizeByte
		case 1:
			size = sizeWord
		case 2:
			size = sizeLong
		default:
			return // 11 selects another instruction
		}
	case hasMoveSize:
		switch ir >> 12 & 3 {
		case 1:
			size = sizeByte
		case 3:
			size = sizeWord
		case 2:
			size = sizeLong
		default:
			return
		}
	case def.sizeFromMode:
		if eaMode(ir) == modeDataRegister {
			size = sizeLong
		} else {
			size = sizeByte
		}
	}

	cycles := def.cycles
	if size == sizeLong && def.cyclesL != 0 {
		cycles = def.cyclesL
	}

	if hasEA {
		mode, reg := eaMode(ir), eaReg(ir)
		if !isValidEA(def.ea, mode, reg) {
			return
		}
		// No instruction can use an address register on a byte operation
		if size == sizeByte && mode == modeAddressRegister {
			return
		}
		if def.timing != timingFixed {
			cycles += eaTime(mode, reg, size)
		}
		if def.rmw && mode != modeDataRegister && mode != modeAddressRegister {
			cycles += 4
		}
	}

	if hasEA2 {
		mode, reg := dstMode(ir), dstReg(ir)
		if !isValidEA(def.ea2, mode, reg) {
			return
		}
		if size == sizeByte && mode == modeAddressRegister {
			return
		}
		if def.timing == timingMove {
			cycles += eaTimeMoveDestination(mode, reg, size)
		}
	}

	action := def.action
	if action == nil {
		if actions[size] == nil {
			actions[size] = def.build(size)
		}
		action = actions[size]
	}

	if ops[ir].action != nil {
		panic(fmt.Sprintf("Opcode $%04x is claimed by both %s and %s",
			ir, ops[ir].name, def.name))
	}

	ops[ir] = opcode{
		name:     def.name,
		size:     size,
		operands: def.operands,
		cycles:   cycles,
		action:   action,
	}
}

// patternBits returns the bits fixed by a pattern and their values
func patternBits(pattern string) (mask uint16, value uint16) {
	if len(pattern) != 16 {
		panic(fmt.Sprintf("Assert failed. The pattern %s is not 16 bits", pattern))
	}

	for i := 0; i < 16; i++ {
		bit := uint16(1) << (15 - i)
		switch pattern[i] {
		case '0':
			mask |= bit
		case '1':
			mask |= bit
			value |= bit
		}
	}
	return mask, value
}

// fillPatternField replaces the occurrences of a character on a pattern with
// the bits of a value, to expand the condition code of Bcc, DBcc and Scc
func fillPatternField(pattern string, field byte, value int) string {
	result := []byte(pattern)
	bits := strings.Count(pattern, string(field))
	found := 0
	for i := 0; i < len(result); i++ {
		if result[i] == field {
			result[i] = '0' + byte(value>>(bits-found-1)&1)
			found++
		}
	}
	return string(result)
}

// conditionalDefs expands the cccc field of a pattern into one definition per
// condition code, skipping the ones on the exclude list
func conditionalDefs(template opcodeDef, prefix string, build func(int) opFunc, skip ...int) []opcodeDef {
	defs := make([]opcodeDef, 0, 16)
	for condition := 0; condition < 16; condition++ {
		if contains(skip, condition) {
			continue
		}
		def := template
		def.name = prefix + conditionNames[condition]
		def.pattern = fillPatternField(template.pattern, 'c', condition)
		def.action = build(condition)
		defs = append(defs, def)
	}
	return defs
}

func contains(values []int, value int) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
