#include "textflag.h"

// Called on the runtime system stack: shift the function pointer out of the
// argument registers and place the null ninth argument in its ABI stack slot.
TEXT ·thermal_call8_trampoline(SB),NOSPLIT,$0-0
 MOVD R0, R12
 MOVD R1, R0
 MOVD R2, R1
 MOVD R3, R2
 MOVD R4, R3
 MOVD R5, R4
 MOVD R6, R5
 MOVD R7, R6
 MOVD R8, R7
 MOVD ZR, (RSP)
 JMP (R12)

GLOBL ·macCall8(SB), RODATA, $8
DATA ·macCall8(SB)/8, $·thermal_call8_trampoline(SB)
