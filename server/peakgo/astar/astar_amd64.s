// Go Assembly (Plan 9 ASM) for amd64
// nodeIndexASM — optimized linear scan of cache[] array.
//
// Cache is a global [512]Node array at cache(SB).
// Node struct: X(0,4B), Z(4,4B), G(8,4B), H(12,4B), Parent(16,4B),
// Closed(20,1B), Open(21,1B). sizeof(Node)=24.
//
// Inputs:
//   x      int32  (x+0(FP))
//   z      int32  (z+4(FP))
//   pc     *PathCache  (pc+8(FP))
// Returns:
//   int32  (ret+16(FP))   index or -1
// Stack frame: $0-20 (no locals, NOSPLIT)

#include "textflag.h"

TEXT ·nodeIndexASM(SB), NOSPLIT, $0-20
	// Load search keys
	MOVL x+0(FP), DI		// DI = x
	MOVL z+4(FP), SI		// SI = z

	// Load pc.nodeCount
	MOVQ pc+8(FP), DX		// DX = &PathCache
	MOVQ 16392(DX), CX		// CX = nodeCount (int64)

	// DX = &cache[0] (package-level variable)
	LEAQ ·cache(SB), DX

	// BX = i = 0
	XORQ BX, BX
	CMPQ BX, CX
	JGE notfound

loop:
	// Compare X
	MOVL (DX), AX
	CMPL AX, DI
	JNE next

	// Compare Z
	MOVL 4(DX), AX
	CMPL AX, SI
	JNE next

	// Found match
	MOVL BX, ret+16(FP)
	RET

next:
	ADDQ $24, DX			// next node
	INCQ BX				// i++
	CMPQ BX, CX
	JL loop

notfound:
	MOVL $-1, ret+16(FP)
	RET

