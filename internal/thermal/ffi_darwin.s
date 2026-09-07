#include "textflag.h"

TEXT ·thermal_dlopen_trampoline(SB),NOSPLIT,$0-0
 JMP thermal_dlopen(SB)
TEXT ·thermal_dlsym_trampoline(SB),NOSPLIT,$0-0
 JMP thermal_dlsym(SB)
TEXT ·thermal_dlclose_trampoline(SB),NOSPLIT,$0-0
 JMP thermal_dlclose(SB)

GLOBL ·macDlopen(SB), RODATA, $8
DATA ·macDlopen(SB)/8, $·thermal_dlopen_trampoline(SB)
GLOBL ·macDlsym(SB), RODATA, $8
DATA ·macDlsym(SB)/8, $·thermal_dlsym_trampoline(SB)
GLOBL ·macDlclose(SB), RODATA, $8
DATA ·macDlclose(SB)/8, $·thermal_dlclose_trampoline(SB)
